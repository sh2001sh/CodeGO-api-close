//go:build pgintegration

package commerce_test

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/internal/commerce"
	"golang.org/x/crypto/bcrypt"
)

type walletEmailCapture struct {
	mu            sync.Mutex
	emails, codes []string
	fail          error
	hook          func() error
}

func (c *walletEmailCapture) SendWalletRecovery(_ context.Context, email, code string) error {
	c.mu.Lock()
	c.emails = append(c.emails, email)
	c.codes = append(c.codes, code)
	fail, hook := c.fail, c.hook
	c.mu.Unlock()
	if hook != nil {
		return hook()
	}
	return fail
}
func (c *walletEmailCapture) latest() (string, string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.codes)
	if n == 0 {
		return "", "", 0
	}
	return c.emails[n-1], c.codes[n-1], n
}

func walletEmailFixture(t *testing.T) (*commerce.Service, *commerce.EmailWalletRecovery, *walletEmailCapture, *pgxpool.Pool, *time.Time) {
	t.Helper()
	_, pool, now := walletFixture(t)
	if _, err := pool.Exec(context.Background(), `UPDATE v3_identity.users SET email=CASE WHEN id=1 THEN 'sender@example.test' ELSE 'recipient@example.test' END,email_verified=true WHERE id IN(1,2)`); err != nil {
		t.Fatal(err)
	}
	capture := &walletEmailCapture{}
	recovery, err := commerce.NewWalletRecovery(pool, capture, commerce.WalletRecoveryConfig{Key: bytes.Repeat([]byte{7}, 32), Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	s := commerce.New(pool, ledger.NewPoster(pool), nil, commerce.Config{Now: func() time.Time { return *now }, WalletRecovery: recovery})
	walletSetup(t, s, 1)
	walletSetup(t, s, 2)
	return s, recovery, capture, pool, now
}
func walletEmailReset(code string) commerce.WalletPasswordInput {
	return commerce.WalletPasswordInput{VerificationMethod: "email", EmailCode: code, NewPaymentPassword: "recovered123", ConfirmPassword: "recovered123"}
}

func TestWalletEmailRecoverySingleUseAndTransactionalRollback(t *testing.T) {
	s, recovery, capture, pool, _ := walletEmailFixture(t)
	ctx := context.Background()
	masked, err := recovery.Send(ctx, 1)
	if err != nil || masked != "sr***@example.test" {
		t.Fatalf("send result %q %v", masked, err)
	}
	email, code, _ := capture.latest()
	if email != "sender@example.test" || len(code) != 6 {
		t.Fatal("sender received wrong recipient or code format")
	}
	var hash, salt []byte
	if err = pool.QueryRow(ctx, `SELECT code_hash,salt FROM v3_commerce.wallet_recovery_codes WHERE user_id=1`).Scan(&hash, &salt); err != nil || len(hash) != 32 || len(salt) != 32 || bytes.Equal(hash, []byte(code)) {
		t.Fatal("recovery code was not stored as keyed salted hash")
	}
	// The failed credential update must roll code consumption back as well.
	_, err = pool.Exec(ctx, `CREATE FUNCTION v3_commerce.reject_wallet_password() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test credential failure'; END $$; CREATE TRIGGER wallet_password_failure BEFORE UPDATE OF password_hash ON v3_commerce.wallet_transfer_security FOR EACH ROW EXECUTE FUNCTION v3_commerce.reject_wallet_password()`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code)); err == nil {
		t.Fatal("failed password update reported success")
	}
	var state string
	if err = pool.QueryRow(ctx, `SELECT state FROM v3_commerce.wallet_recovery_codes WHERE user_id=1`).Scan(&state); err != nil || state != "active" {
		t.Fatalf("rolled-back reset consumed code %q %v", state, err)
	}
	if _, err = pool.Exec(ctx, `DROP TRIGGER wallet_password_failure ON v3_commerce.wallet_transfer_security`); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code)) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, commerce.ErrWalletEmailCodeInvalid) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("code was reused %d times", success)
	}
	var passwordHash string
	if err = pool.QueryRow(ctx, `SELECT password_hash FROM v3_commerce.wallet_transfer_security WHERE user_id=1`).Scan(&passwordHash); err != nil || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("recovered123")) != nil {
		t.Fatal("code did not authorize the password update")
	}
}

func TestWalletEmailRecoveryBoundUserAddressAndExpiration(t *testing.T) {
	s, recovery, capture, pool, now := walletEmailFixture(t)
	ctx := context.Background()
	if _, err := recovery.Send(ctx, 1); err != nil {
		t.Fatal(err)
	}
	_, code, _ := capture.latest()
	if err := s.ConfigureWalletPassword(ctx, 2, walletEmailReset(code)); !errors.Is(err, commerce.ErrWalletEmailCodeInvalid) {
		t.Fatalf("cross-user recovery accepted %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET email='changed@example.test' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code)); !errors.Is(err, commerce.ErrWalletEmailCodeInvalid) {
		t.Fatalf("changed-email recovery accepted %v", err)
	}
	*now = now.Add(time.Minute)
	if _, err := recovery.Send(ctx, 1); err != nil {
		t.Fatal(err)
	}
	_, code, _ = capture.latest()
	*now = now.Add(10 * time.Minute)
	if err := s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code)); !errors.Is(err, commerce.ErrWalletEmailCodeInvalid) {
		t.Fatalf("expired code accepted %v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM v3_commerce.wallet_recovery_codes WHERE user_id=1`).Scan(&state); err != nil || state != "expired" {
		t.Fatalf("expired state %q %v", state, err)
	}
}

func TestWalletEmailRecoveryRejectedAttemptsPersistAndCannotBypassLock(t *testing.T) {
	s, recovery, capture, pool, now := walletEmailFixture(t)
	ctx := context.Background()
	if _, err := recovery.Send(ctx, 1); err != nil {
		t.Fatal(err)
	}
	_, code, _ := capture.latest()
	for i := 1; i <= 5; i++ {
		want := commerce.ErrWalletEmailCodeInvalid
		if i == 5 {
			want = commerce.ErrWalletEmailCodeLocked
		}
		if err := s.ConfigureWalletPassword(ctx, 1, walletEmailReset("xxxxxx")); !errors.Is(err, want) {
			t.Fatalf("attempt %d %v", i, err)
		}
	}
	var failures int
	if err := pool.QueryRow(ctx, `SELECT failed_attempts FROM v3_commerce.wallet_recovery_codes WHERE user_id=1`).Scan(&failures); err != nil || failures != 5 {
		t.Fatalf("failed attempts rolled back %d %v", failures, err)
	}
	if err := s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code)); !errors.Is(err, commerce.ErrWalletEmailCodeLocked) {
		t.Fatalf("correct code bypassed lock %v", err)
	}
	*now = now.Add(time.Minute)
	if _, err := recovery.Send(ctx, 1); !errors.Is(err, commerce.ErrWalletEmailCodeLocked) {
		t.Fatalf("new code bypassed lock %v", err)
	}
	*now = now.Add(30 * time.Minute)
	if _, err := recovery.Send(ctx, 1); err != nil {
		t.Fatal(err)
	}
	_, code, _ = capture.latest()
	if err := s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code)); err != nil {
		t.Fatal(err)
	}
}

func TestWalletEmailRecoveryDeliveryFailureAndSendCooldown(t *testing.T) {
	s, recovery, capture, pool, now := walletEmailFixture(t)
	ctx := context.Background()
	capture.fail = errors.New("test SMTP offline")
	if _, err := recovery.Send(ctx, 1); !errors.Is(err, commerce.ErrWalletEmailDelivery) {
		t.Fatalf("fake delivery success %v", err)
	}
	_, code, _ := capture.latest()
	if err := s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code)); !errors.Is(err, commerce.ErrWalletEmailCodeInvalid) {
		t.Fatalf("failed delivery code accepted %v", err)
	}
	if _, err := recovery.Send(ctx, 1); !errors.Is(err, commerce.ErrWalletEmailRateLimited) {
		t.Fatalf("failed-send cooldown bypass %v", err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM v3_commerce.wallet_recovery_codes WHERE user_id=1`).Scan(&state); err != nil || state != "failed" {
		t.Fatalf("delivery state %q %v", state, err)
	}
	*now = now.Add(time.Minute)
	capture.fail = nil
	var wg sync.WaitGroup
	results := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := recovery.Send(ctx, 1); results <- err }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, commerce.ErrWalletEmailRateLimited) {
			t.Fatal(err)
		}
	}
	_, _, calls := capture.latest()
	if success != 1 || calls != 2 {
		t.Fatalf("parallel send bypassed rate limit: %d %d", success, calls)
	}
}

func TestWalletEmailRecoveryRequiresTrustedVerifiedEmailAndFencesDelivery(t *testing.T) {
	s, recovery, capture, pool, _ := walletEmailFixture(t)
	ctx := context.Background()
	if _, err := recovery.Send(ctx, 3); !errors.Is(err, commerce.ErrWalletEmailRequired) {
		t.Fatalf("unbound mailbox accepted %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET email_verified=false,settings='{"email_verified":true}' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := recovery.Send(ctx, 1); !errors.Is(err, commerce.ErrWalletEmailUnverified) {
		t.Fatalf("untrusted settings asserted verified address %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE v3_identity.users SET email_verified=true WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	capture.hook = func() error {
		_, err := pool.Exec(ctx, `UPDATE v3_identity.users SET email='replacement@example.test',email_verified=false WHERE id=1`)
		return err
	}
	if _, err := recovery.Send(ctx, 1); !errors.Is(err, commerce.ErrWalletEmailCodeInvalid) {
		t.Fatalf("stale delivered generation activated %v", err)
	}
	_, code, _ := capture.latest()
	if err := s.ConfigureWalletPassword(ctx, 1, walletEmailReset(code)); !errors.Is(err, commerce.ErrWalletEmailUnverified) {
		t.Fatalf("stale code reset password %v", err)
	}
}
