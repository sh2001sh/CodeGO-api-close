package legacy

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type onlineSourceFenceContextKey struct{}
type onlineSourceFenceKeeper struct {
	source *pgxpool.Pool
	conn   *pgxpool.Conn
}

func onlineInheritedSourceFence(ctx context.Context, source *pgxpool.Pool) (bool, error) {
	keeper, _ := ctx.Value(onlineSourceFenceContextKey{}).(*onlineSourceFenceKeeper)
	if keeper == nil || keeper.source != source {
		return false, nil
	}
	if keeper.conn == nil {
		return false, errors.New("legacy: online source fence keeper is missing")
	}
	var held bool
	if err := keeper.conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND pid=pg_backend_pid() AND classid=0 AND objid=738301032 AND objsubid=1 AND mode='ShareLock' AND granted)`).Scan(&held); err != nil {
		return false, err
	}
	if !held {
		return false, errors.New("legacy: online source fence keeper lost its lock")
	}
	return true, nil
}

// Seal and Unseal take the exclusive form of this lock in the source database.
// A session lock spans the several source snapshots and the final target commit;
// a target-only lock cannot prevent another source connection reopening V2.
func onlineHoldSourceFence(ctx context.Context, source *pgxpool.Pool) (*pgxpool.Conn, error) {
	if source == nil {
		return nil, errors.New("legacy: online source is required")
	}
	conn, err := source.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var acquired bool
	if err = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock_shared(738301032)").Scan(&acquired); err != nil || !acquired {
		closeOnlineConnection(conn)
		if err == nil {
			err = errors.New("legacy: source seal is being changed; retry finalization after it completes")
		}
		return nil, err
	}
	return conn, nil
}

func (m *Importer) FinalizeOnline(ctx context.Context, opts OnlineOptions) (Report, error) {
	ctx = m.historyContext(ctx)
	r := Report{Counts: map[string]int64{}, Amounts: map[string]string{}}
	if opts.SourceAdmin == nil {
		return r, errors.New("legacy: finalization requires source acknowledgement connection")
	}
	if m.source == nil || m.pool == nil {
		return r, errors.New("legacy: independent source and target required")
	}
	if err := ValidateOnlineCaptureSource(ctx, m.source, opts.SourceAdmin); err != nil {
		return r, err
	}
	fence, err := onlineHoldSourceFence(ctx, m.source)
	if err != nil {
		return r, err
	}
	defer closeOnlineConnection(fence)
	source, err := m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	capture, err := ValidateOnlineCapture(ctx, source, opts.RunID)
	if err == nil && !capture.Sealed {
		err = errors.New("legacy: online source must be explicitly sealed before finalization")
	}
	_ = source.Rollback(ctx)
	if err != nil {
		return r, err
	}
	// A large backlog belongs in the online stage, not the outage. Keep the
	// fence intact on refusal; callers may sync further or explicitly unseal.
	var pending int64
	if err = m.source.QueryRow(ctx, "SELECT count(*) FROM v3_migration_capture.events WHERE NOT acked").Scan(&pending); err != nil {
		return r, err
	}
	if pending > 16384 {
		return r, errors.New("legacy: online backlog exceeds the final-window budget; continue syncing before finalization")
	}
	for pending > 0 {
		sync, err := m.SyncOnline(ctx, opts)
		if err != nil {
			return r, err
		}
		if sync.Acknowledged == 0 && sync.Pending > 0 {
			return r, errors.New("legacy: final online sync made no progress")
		}
		pending = sync.Pending
	}
	conn, err := m.onlineConnection(ctx)
	if err != nil {
		return r, err
	}
	defer closeOnlineConnection(conn)
	source, err = m.source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return r, err
	}
	defer func() { _ = source.Rollback(ctx) }()
	target, err := conn.Begin(ctx)
	if err != nil {
		return r, err
	}
	defer func() { _ = target.Rollback(ctx) }()
	if err = onlineAuthorize(ctx, target, opts.RunID); err != nil {
		return r, err
	}
	sources, _, err := onlineBindings(ctx, source, target, opts.RunID)
	if err != nil {
		return r, err
	}
	if err = onlineRequireQuiescent(ctx, source); err != nil {
		return r, err
	}
	if _, err = loadOnlineProjector(ctx, source, target, sources); err != nil {
		return r, err
	}
	view, err := onlineLoadView(ctx, target, opts.RunID)
	if err != nil {
		return r, err
	}
	if err = target.Commit(ctx); err != nil {
		return r, err
	}
	if err = source.Commit(ctx); err != nil {
		return r, err
	}
	// The normal importer retains its single atomic commit, all financial drain
	// checks, restored-secret validation and runtime initialization.
	finalCtx := context.WithValue(ctx, onlineSourceFenceContextKey{}, &onlineSourceFenceKeeper{source: m.source, conn: fence})
	return m.Import(context.WithValue(finalCtx, onlineContextKey{}, view), true)
}

func onlineAdopt(ctx context.Context, target pgx.Tx, specs []onlineSpec, view *onlineView) error {
	if err := onlineAuthorize(ctx, target, view.runID); err != nil {
		return err
	}
	if err := onlineLockTargetSchema(ctx, target); err != nil {
		return err
	}
	if err := onlineValidateTargetSchema(ctx, target); err != nil {
		return err
	}
	for _, table := range onlineSortedTables(specs) {
		if table == "v3_billing.historical_accounts" {
			continue
		} // final native mappings are small
		parts := strings.Split(table, ".")
		schema, name := parts[0], parts[1]
		var populated bool
		if err := target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+")").Scan(&populated); err != nil {
			return err
		}
		if populated {
			return errors.New("legacy: native target acquired activity before online adoption")
		}
		if table == "v3_billing.usage_logs" {
			if _, err := target.Exec(ctx, "ALTER TABLE v3_billing.usage_logs_default SET SCHEMA v3_migration_previous"); err != nil {
				return err
			}
		}
		if _, err := target.Exec(ctx, "ALTER TABLE "+pgx.Identifier{schema, name}.Sanitize()+" SET SCHEMA v3_migration_previous"); err != nil {
			return err
		}
		if _, err := target.Exec(ctx, "DROP TRIGGER online_staging_guard ON "+onlineStage(table)); err != nil {
			return err
		}
		if table == "v3_billing.usage_logs" {
			if _, err := target.Exec(ctx, "DROP TRIGGER online_staging_guard ON "+onlineStage(table+"_default")); err != nil {
				return err
			}
			if _, err := target.Exec(ctx, "ALTER TABLE "+onlineStage(table+"_default")+" SET SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
				return err
			}
			if _, err := target.Exec(ctx, "ALTER TABLE "+pgx.Identifier{schema, strings.ReplaceAll(table+"_default", ".", "__")}.Sanitize()+" RENAME TO usage_logs_default"); err != nil {
				return err
			}
		}
		if _, err := target.Exec(ctx, "ALTER TABLE "+onlineStage(table)+" SET SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
			return err
		}
		if _, err := target.Exec(ctx, "ALTER TABLE "+pgx.Identifier{schema, strings.ReplaceAll(table, ".", "__")}.Sanitize()+" RENAME TO "+pgx.Identifier{name}.Sanitize()); err != nil {
			return err
		}
	}
	return onlineRestoreObjectNames(ctx, target)
}

func onlineRestoreRelationships(ctx context.Context, target pgx.Tx, specs []onlineSpec) error {
	// Closed/unmapped funding accounts intentionally remain nullable, matching
	// resolveFundingAccount against the final live index. Available lots may
	// never lose their account mapping.
	for _, spec := range specs {
		if spec.name != "funding_lots" && spec.name != "funding_allocations" {
			continue
		}
		table := "v3_billing." + spec.name
		if spec.name == "funding_lots" {
			var invalid bool
			if err := target.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" l WHERE l.remaining_amount>0 AND NOT EXISTS(SELECT 1 FROM v3_billing.accounts a WHERE a.id=l.account_id))").Scan(&invalid); err != nil {
				return err
			}
			if invalid {
				return errors.New("legacy: spendable staged funding lacks its final live account")
			}
		}
		if _, err := target.Exec(ctx, "UPDATE "+table+" l SET account_id=NULL WHERE account_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM v3_billing.accounts a WHERE a.id=l.account_id)"); err != nil {
			return err
		}
	}
	rows, err := target.Query(ctx, "SELECT table_name,constraint_name,definition FROM v3_migration_online.foreign_keys ORDER BY table_name,constraint_name")
	if err != nil {
		return err
	}
	type fk struct{ table, name, definition string }
	var constraints []fk
	for rows.Next() {
		var c fk
		if err = rows.Scan(&c.table, &c.name, &c.definition); err != nil {
			rows.Close()
			return err
		}
		constraints = append(constraints, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, c := range constraints {
		if c.table == "v3_billing.historical_accounts" {
			continue
		}
		if _, err = target.Exec(ctx, "ALTER TABLE "+pgx.Identifier(strings.Split(c.table, ".")).Sanitize()+" ADD CONSTRAINT "+pgx.Identifier{c.name}.Sanitize()+" "+c.definition); err != nil {
			return err
		}
	}
	return nil
}
