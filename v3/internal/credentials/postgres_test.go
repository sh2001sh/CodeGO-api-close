//go:build pgintegration

package credentials

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/migrations"
)

func credentialTestStore(t *testing.T) (*PGStore, *catalog.AESGCM) {
	t.Helper()
	dsn := os.Getenv("V3_CREDENTIAL_TEST_PG_DSN")
	if dsn == "" {
		dsn = os.Getenv("V3_TEST_PG_DSN")
	}
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// Integration packages run serially against a disposable database. Previous
	// fixtures may insert explicit IDs without advancing identity sequences, and
	// migrations may add new schemas. Rebuild the entire v3 fixture every time.
	rows, err := pool.Query(ctx, `SELECT nspname FROM pg_namespace WHERE left(nspname,3)='v3_'`)
	if err != nil {
		t.Fatal(err)
	}
	var schemas []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		schemas = append(schemas, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range schemas {
		if _, err = pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{name}.Sanitize()+" CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		sql, err := migrations.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	cipher, err := catalog.NewAESGCM(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return NewPGStore(pool, cipher), cipher
}

func TestPGStoreEncryptsRotationAndRejectsStaleOrDisabledCredentials(t *testing.T) {
	store, cipher := credentialTestStore(t)
	ctx := context.Background()
	var channelID, credentialID int64
	if err := store.pool.QueryRow(ctx, `INSERT INTO v3_catalog.channels(name,provider) VALUES ('credential-pool-integration','codex') RETURNING id`).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = store.pool.Exec(ctx, `DELETE FROM v3_catalog.channels WHERE id=$1`, channelID) })
	sealed, err := cipher.Encrypt([]byte(`{"refresh_token":"old-refresh"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.pool.QueryRow(ctx, `INSERT INTO v3_catalog.channel_credentials(channel_id,kind,secret,expires_at)
		VALUES ($1,'oauth',$2,now()) RETURNING id`, channelID, sealed).Scan(&credentialID); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var observed Credential
	for _, c := range list {
		if c.ID == credentialID {
			observed = c
		}
	}
	if observed.ID == 0 {
		t.Fatal("expiring OAuth credential not listed")
	}
	calls := 0
	r := refreshFunc(func(_ context.Context, c Credential) (Credential, error) {
		calls++
		c.Secret = []byte(`{"access_token":"new-access","refresh_token":"rotated-refresh"}`)
		c.ExpiresAt = time.Now().Add(time.Hour)
		c.Fingerprint = Fingerprint{UserAgent: "persisted-ua", TLSProfile: "chrome"}
		return c, nil
	})
	fresh, err := store.Refresh(ctx, observed, r)
	if err != nil {
		t.Fatal(err)
	}
	var encrypted []byte
	var fp string
	if err = store.pool.QueryRow(ctx, `SELECT secret,fingerprint->>'user_agent' FROM v3_catalog.channel_credentials WHERE id=$1`, credentialID).Scan(&encrypted, &fp); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("new-access")) || bytes.Equal(encrypted, fresh.Secret) {
		t.Fatal("plaintext token persisted")
	}
	plain, err := cipher.Decrypt(encrypted)
	if err != nil || !bytes.Equal(plain, fresh.Secret) || fp != "persisted-ua" {
		t.Fatalf("rotated secret or fingerprint not persisted: err=%v", err)
	}
	if _, err = store.Refresh(ctx, observed, r); !errors.Is(err, ErrChanged) {
		t.Fatalf("stale rotation accepted: %v", err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE v3_catalog.channel_credentials SET status='disabled' WHERE id=$1`, credentialID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Refresh(ctx, fresh, r); !errors.Is(err, ErrChanged) {
		t.Fatalf("disabled rotation accepted: %v", err)
	}
	if calls != 1 {
		t.Fatalf("token endpoint called %d times, want 1", calls)
	}
	var invalidations int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity='catalog' AND entity_id=$1`, strconv.FormatInt(channelID, 10)).Scan(&invalidations); err != nil {
		t.Fatal(err)
	}
	if invalidations < 3 {
		t.Fatal("refresh did not invalidate catalog")
	}
}

func TestPGStoreLeadershipIsExclusiveAndTransfersAfterCancellation(t *testing.T) {
	store, _ := credentialTestStore(t)
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	enteredA, enteredB := make(chan struct{}), make(chan struct{})
	doneA, doneB := make(chan error, 1), make(chan error, 1)
	go func() {
		doneA <- store.RunExclusive(ctxA, func(ctx context.Context) error { close(enteredA); <-ctx.Done(); return nil })
	}()
	select {
	case <-enteredA:
	case <-time.After(5 * time.Second):
		t.Fatal("leader A not acquired")
	}
	go func() {
		doneB <- store.RunExclusive(ctxB, func(ctx context.Context) error { close(enteredB); <-ctx.Done(); return nil })
	}()
	select {
	case <-enteredB:
		t.Fatal("two refresh leaders acquired lock")
	case <-time.After(100 * time.Millisecond):
	}
	cancelA()
	if err := <-doneA; err != nil {
		t.Fatal(err)
	}
	select {
	case <-enteredB:
	case <-time.After(5 * time.Second):
		t.Fatal("leadership not transferred")
	}
	cancelB()
	if err := <-doneB; err != nil {
		t.Fatal(err)
	}
}

// Rotating refresh tokens cannot be rolled back at the remote provider. Once
// exchanged, cancellation must not discard the only usable replacement token.
func TestPGStorePersistsRotatedTokenAfterExchangeContextIsCanceled(t *testing.T) {
	store, cipher := credentialTestStore(t)
	ctx := context.Background()
	var channelID, credentialID int64
	if err := store.pool.QueryRow(ctx, `INSERT INTO v3_catalog.channels(name,provider) VALUES ('credential-canceled-exchange','codex') RETURNING id`).Scan(&channelID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = store.pool.Exec(ctx, `DELETE FROM v3_catalog.channels WHERE id=$1`, channelID) })
	sealed, err := cipher.Encrypt([]byte(`{"refresh_token":"old-refresh"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.pool.QueryRow(ctx, `INSERT INTO v3_catalog.channel_credentials(channel_id,kind,secret,expires_at)
		VALUES ($1,'oauth',$2,now()) RETURNING id`, channelID, sealed).Scan(&credentialID); err != nil {
		t.Fatal(err)
	}
	list, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var observed Credential
	for _, c := range list {
		if c.ID == credentialID {
			observed = c
		}
	}
	refreshCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	freshSecret := []byte(`{"access_token":"new-access","refresh_token":"only-usable-refresh"}`)
	refresher := refreshFunc(func(_ context.Context, c Credential) (Credential, error) {
		c.Secret = freshSecret
		c.ExpiresAt = time.Now().Add(time.Hour)
		cancel() // remote exchange completed exactly as shutdown/timeout arrived
		return c, nil
	})
	if _, err = store.Refresh(refreshCtx, observed, refresher); err != nil {
		t.Fatalf("rotated token discarded on cancellation: %v", err)
	}
	var encrypted []byte
	if err = store.pool.QueryRow(ctx, `SELECT secret FROM v3_catalog.channel_credentials WHERE id=$1`, credentialID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	plain, err := cipher.Decrypt(encrypted)
	if err != nil || !bytes.Equal(plain, freshSecret) {
		t.Fatalf("replacement token not durable: %v", err)
	}
}

func TestCredentialFixtureResetsPriorExplicitIDsAndAllV3Schemas(t *testing.T) {
	prior, _ := credentialTestStore(t)
	ctx := context.Background()
	if _, err := prior.pool.Exec(ctx, `INSERT INTO v3_catalog.channels(id,name,provider) VALUES (1,'prior-explicit-id','openai')`); err != nil {
		t.Fatal(err)
	}
	if _, err := prior.pool.Exec(ctx, `CREATE SCHEMA v3_prior_fixture; CREATE TABLE v3_prior_fixture.stale(id bigint)`); err != nil {
		t.Fatal(err)
	}
	if _, err := prior.pool.Exec(ctx, `CREATE SCHEMA credential_fixture_sentinel`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = prior.pool.Exec(ctx, `DROP SCHEMA IF EXISTS credential_fixture_sentinel CASCADE`) })
	fresh, _ := credentialTestStore(t)
	var id int64
	if err := fresh.pool.QueryRow(ctx, `INSERT INTO v3_catalog.channels(name,provider) VALUES ('fresh-sequence','codex') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("previous explicit IDs contaminated credential fixture: %v", err)
	}
	if id != 1 {
		t.Fatalf("sequence was not reset: id=%d, want 1", id)
	}
	var stale, sentinel bool
	if err := fresh.pool.QueryRow(ctx, `SELECT to_regnamespace('v3_prior_fixture') IS NOT NULL,to_regnamespace('credential_fixture_sentinel') IS NOT NULL`).Scan(&stale, &sentinel); err != nil {
		t.Fatal(err)
	}
	if stale || !sentinel {
		t.Fatalf("wrong schema reset scope: v3_prior_fixture=%v sentinel=%v", stale, sentinel)
	}
}
