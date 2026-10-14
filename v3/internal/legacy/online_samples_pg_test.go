//go:build pgintegration

package legacy

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestOnlineSampledAcceptanceDistinctAndRejectsBadMoney(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "wrong_staged_money"}[corrupt], func(t *testing.T) {
			m, source, target, opts := onlineMigrationFixture(t, true)
			ctx := context.Background()
			if _, err := m.PrepareOnline(ctx, opts, true); err != nil {
				t.Fatal(err)
			}
			if _, err := m.CopyOnline(ctx, opts); err != nil {
				t.Fatal(err)
			}
			onlineMigrationSync(t, m, opts)
			if _, err := m.AcceptOnlineSamples(ctx, opts); err == nil {
				t.Fatal("accepted without explicit waiver")
			}
			if corrupt {
				tx, err := target.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if err = onlineAuthorize(ctx, tx, opts.RunID); err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(ctx, "UPDATE "+onlineStage("v3_billing.funding_lots")+" SET original_amount=original_amount+2"); err != nil {
					t.Fatal(err)
				}
				if err = tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			}
			opts.SampledAcceptance = true
			r, err := m.AcceptOnlineSamples(ctx, opts)
			if corrupt {
				if err == nil {
					t.Fatal("sample accepted changed financial projection")
				}
				return
			}
			if err != nil || !r.Applied || r.Phase != "sampled" || r.HistoryVerification != "sampled" {
				t.Fatalf("sample: %+v %v", r, err)
			}
			var marker, verified, native bool
			if err = target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM v3_migration_online.totals WHERE name='verification.financial_history'),(SELECT verified_at IS NOT NULL FROM v3_migration_online.run WHERE singleton),EXISTS(SELECT 1 FROM v3_billing.accounts)").Scan(&marker, &verified, &native); err != nil {
				t.Fatal(err)
			}
			if marker || verified || native {
				t.Fatal("samples claimed full verification or opened money")
			}
			tx, err := target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = onlineLoadView(ctx, tx, opts.RunID); err == nil {
				t.Fatal("ordinary finalizer accepted sampled phase")
			}
			_ = tx.Rollback(ctx)
			tx, err = target.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			v, err := onlineLoadAcceptedView(ctx, tx, opts.RunID, true)
			if err != nil || !v.sampled {
				t.Fatalf("explicit sampled view: %v", err)
			}
			_ = tx.Rollback(ctx)
			if _, err = m.SyncOnline(ctx, opts); err != nil {
				t.Fatal(err)
			}
			if _, err = m.FinalizeOnline(ctx, opts); err == nil {
				t.Fatal("sampled finalizer accepted unsealed source")
			}
			if _, err = SealOnlineCapture(ctx, source, opts.RunID); err != nil {
				t.Fatal(err)
			}
			final, err := m.FinalizeOnline(ctx, opts)
			if err != nil || !final.Applied {
				t.Fatalf("sampled finalization %+v %v", final, err)
			}
		})
	}
}
