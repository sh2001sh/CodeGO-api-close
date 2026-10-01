//go:build pgintegration

package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedOIDCFixture(t *testing.T, source *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	if _, err := source.Exec(ctx, `CREATE TABLE migration_source.codego_oidc_authorization_codes(code_hash text,client_id text,user_id bigint,redirect_uri text,scope text,nonce text,code_challenge text,expires_at timestamptz,used_at timestamptz);
		CREATE TABLE migration_source.codego_oidc_access_tokens(token_hash text,client_id text,user_id bigint,scope text,expires_at timestamptz,revoked_at timestamptz);`); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"unused-code", "used-code"} {
		digest := sha256.Sum256([]byte(code))
		var used *time.Time
		if code == "used-code" {
			when := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			used = &when
		}
		if _, err := source.Exec(ctx, `INSERT INTO migration_source.codego_oidc_authorization_codes VALUES($1,'client',7,'https://client.invalid/cb','openid profile','nonce','challenge','2099-01-01T00:00:00Z',$2)`, hex.EncodeToString(digest[:]), used); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256([]byte("revoked-token"))
	if _, err := source.Exec(ctx, `INSERT INTO migration_source.codego_oidc_access_tokens VALUES($1,'client',7,'openid profile','2099-01-01T00:00:00Z','2024-01-01T00:00:00Z')`, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
}

func TestOIDCMigrationKeepsUsedCodesAndRevokedTokensUnusable(t *testing.T) {
	source, target, crypto := importTestDB(t)
	seedOIDCFixture(t, source)
	ctx := context.Background()
	importer := NewImporter(readonlySource(t, source), target, crypto)
	if report, err := importer.Import(ctx, true); err != nil || !report.Applied {
		t.Fatalf("import=%+v err=%v", report, err)
	}
	used := sha256.Sum256([]byte("used-code"))
	var expired, revoked bool
	if err := target.QueryRow(ctx, `SELECT expires_at<now() FROM v3_identity.oidc_codes WHERE code_hash=$1`, used[:]).Scan(&expired); err != nil || !expired {
		t.Fatalf("used code revived: expired=%t err=%v", expired, err)
	}
	token := sha256.Sum256([]byte("revoked-token"))
	if err := target.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM v3_identity.oidc_tokens WHERE token_hash=$1`, token[:]).Scan(&revoked); err != nil || !revoked {
		t.Fatalf("revoked token revived: revoked=%t err=%v", revoked, err)
	}
	if _, err := importer.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec(ctx, `UPDATE migration_source.codego_oidc_access_tokens SET token_hash='invalid-digest'`); err != nil {
		t.Fatal(err)
	}
	if _, err := importer.Import(ctx, true); err == nil {
		t.Fatal("invalid digest accepted")
	}
}
