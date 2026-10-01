//go:build pgintegration

package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Run with: V3_TEST_PG_DSN=postgres://... go test -tags=pgintegration ./migrations/
// The DSN must point at a disposable database; the test drops the v3 schemas.
func connect(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("V3_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("V3_TEST_PG_DSN not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	if _, err := conn.Exec(ctx, `DO $$ DECLARE s record; BEGIN
		FOR s IN SELECT nspname FROM pg_namespace WHERE left(nspname,3)='v3_' LOOP
			EXECUTE format('DROP SCHEMA %I CASCADE', s.nspname);
		END LOOP;
	END $$`); err != nil {
		t.Fatal(err)
	}
	names, _ := Files()
	for _, name := range names {
		sql, _ := Read(name)
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return conn
}

func TestTriggersEnqueueInvalidations(t *testing.T) {
	conn := connect(t)
	ctx := context.Background()
	mustExec(t, conn, `INSERT INTO v3_identity.users (id, username) VALUES (1, 'alice')`)
	mustExec(t, conn, `UPDATE v3_identity.users SET last_login_at = now() WHERE id = 1`) // must not enqueue
	mustExec(t, conn, `UPDATE v3_identity.users SET group_name = 'vip' WHERE id = 1`)
	var identityChanges int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM v3_platform.cache_invalidation_outbox WHERE entity='user' AND entity_id='1'`).Scan(&identityChanges); err != nil || identityChanges != 2 {
		t.Fatalf("identity changes=%d err=%v; login time must not invalidate", identityChanges, err)
	}
	mustExec(t, conn, `INSERT INTO v3_catalog.groups (name) VALUES ('vip')`)

	var users, catalog int
	if err := conn.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE entity = 'user' AND entity_id = '1'),
		count(*) FILTER (WHERE entity = 'catalog' AND entity_id = 'vip')
		FROM v3_platform.cache_invalidation_outbox`).Scan(&users, &catalog); err != nil {
		t.Fatal(err)
	}
	if users != 3 || catalog != 1 { // insert + user group + official usable-group policy
		t.Fatalf("outbox rows: user=%d catalog=%d; want 3 and 1", users, catalog)
	}
}

func TestLedgerConstraints(t *testing.T) {
	conn := connect(t)
	mustExec(t, conn, `INSERT INTO v3_billing.accounts (owner_type, owner_id, kind) VALUES ('user', 1, 'wallet')`)
	mustExec(t, conn, `INSERT INTO v3_billing.ledger_entries (account_id, amount, balance_after, kind, operation_id)
		VALUES (1, 2000000, 2000000, 'opening', 'migration:user:1')`)

	mustFail(t, conn, "duplicate operation_id", `INSERT INTO v3_billing.ledger_entries
		(account_id, amount, balance_after, kind, operation_id) VALUES (1, 5, 5, 'topup', 'migration:user:1')`)
	mustFail(t, conn, "zero amount", `INSERT INTO v3_billing.ledger_entries
		(account_id, amount, balance_after, kind, operation_id) VALUES (1, 0, 0, 'topup', 'x')`)
	mustFail(t, conn, "closed reservation without closed_at", `INSERT INTO v3_billing.reservations
		(request_id, account_id, amount, state, expires_at) VALUES ('r1', 1, 10, 'settled', now())`)
	mustExec(t, conn, `INSERT INTO v3_billing.reservations (request_id, account_id, amount, expires_at)
		VALUES ('r2', 1, 10, now() + interval '5 minutes')`)
}

func mustExec(t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func mustFail(t *testing.T, conn *pgx.Conn, what, sql string) {
	t.Helper()
	if _, err := conn.Exec(context.Background(), sql); err == nil {
		t.Fatalf("%s: expected a constraint violation", what)
	}
}
