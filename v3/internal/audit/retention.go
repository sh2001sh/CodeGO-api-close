package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Financial state is authoritative; a request's HTTP status alone cannot
// release evidence still needed for recovery or delayed batch reconciliation.
func pendingRequest(alias string) string {
	id := alias + ".request_id"
	return "(EXISTS(SELECT 1 FROM v3_billing.reservations p WHERE p.request_id=" + id + " AND p.state='open') OR EXISTS(SELECT 1 FROM v3_workflow.tasks p WHERE p.id=" + id + " AND (p.cost_state='reserved' OR p.status NOT IN ('completed','failed'))) OR EXISTS(SELECT 1 FROM v3_channelmarket.settlements p WHERE p.request_id=" + id + " AND p.status='pending') OR EXISTS(SELECT 1 FROM v3_channelmarket.batch_test_items p WHERE p.request_id<>'' AND p.request_id=" + id + " AND (p.status IN ('queued','running') OR (p.status='failed' AND NOT p.log_created))))"
}

func terminalAudit(alias string) string {
	return alias + ".status IN ('succeeded','failed','canceled','cancelled','completed') AND " + alias + ".completed_at > '1970-01-01'::timestamptz"
}

// CleanupHistoriesBefore removes at most limit rows per ordinary history in a
// short atomic transaction. It never changes accounts, ledgers or settlements.
// Deleted usage becomes compact totals so lifetime Key usage does not decrease.
func CleanupHistoriesBefore(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, cutoff time.Time, limit int) (int64, error) {
	if cutoff.IsZero() || limit < 1 || limit > 5000 {
		return 0, fmt.Errorf("audit: invalid history cleanup boundary or limit")
	}
	var deleted int64
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout='200ms'; SET LOCAL statement_timeout='5s'"); err != nil {
			return err
		}
		var acquired bool
		if err := tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended('v3:history_retention',0))").Scan(&acquired); err != nil {
			return err
		}
		if !acquired {
			return nil
		}
		usageSQL := `WITH expired AS (
		 SELECT l.tableoid AS part,l.ctid FROM v3_billing.usage_logs l
		 WHERE l.created_at<$1 AND l.terminal IN ('completed','succeeded','success','failed','released','expired','cancelled','canceled') AND NOT ` + pendingRequest("l") + `
		 AND NOT EXISTS(SELECT 1 FROM v3_audit.request_audits r WHERE r.request_id=l.request_id AND (` + terminalAudit("r") + `) IS NOT TRUE)
		 ORDER BY l.created_at,l.id LIMIT $2 FOR UPDATE OF l SKIP LOCKED
		), removed AS (
		 DELETE FROM v3_billing.usage_logs l USING expired e WHERE l.tableoid=e.part AND l.ctid=e.ctid RETURNING l.user_id,l.key_id,l.amount
		), totals AS (
		 INSERT INTO v3_billing.retired_usage_totals(user_id,key_id,amount)
		 SELECT user_id,key_id,sum(amount)::bigint FROM removed GROUP BY user_id,key_id
		 ON CONFLICT(user_id,key_id) DO UPDATE SET amount=v3_billing.retired_usage_totals.amount+EXCLUDED.amount RETURNING 1
		) SELECT count(*) FROM removed`
		var n int64
		if err := tx.QueryRow(ctx, usageSQL, cutoff, limit).Scan(&n); err != nil {
			return err
		}
		deleted += n
		rows, err := tx.Query(ctx, `SELECT r.request_id FROM v3_audit.request_audits r WHERE r.created_at<$1 AND r.started_at<$1 AND r.completed_at<$1 AND `+terminalAudit("r")+` AND NOT `+pendingRequest("r")+`
		 AND NOT EXISTS(SELECT 1 FROM v3_audit.request_attempt_audits a WHERE a.request_id=r.request_id AND (a.created_at IS NULL OR a.started_at IS NULL OR a.created_at>=$1 OR a.started_at>=$1 OR a.completed_at>=$1 OR (`+terminalAudit("a")+`) IS NOT TRUE)) ORDER BY r.created_at,r.request_id LIMIT $2 FOR UPDATE OF r SKIP LOCKED`, cutoff, limit)
		if err != nil {
			return err
		}
		var parents []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			parents = append(parents, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(parents) > 0 {
			// A single parent can own more than limit attempts. Remove only one
			// bounded child batch, then remove parents whose families are empty.
			for _, query := range []string{
				`WITH expired AS(SELECT a.attempt_id FROM v3_audit.request_attempt_audits a WHERE a.request_id=ANY($1::text[]) ORDER BY a.attempt_id LIMIT $2 FOR UPDATE OF a SKIP LOCKED) DELETE FROM v3_audit.request_attempt_audits a USING expired e WHERE a.attempt_id=e.attempt_id`,
				`DELETE FROM v3_audit.request_audits r WHERE r.request_id=ANY($1::text[]) AND NOT EXISTS(SELECT 1 FROM v3_audit.request_attempt_audits a WHERE a.request_id=r.request_id) AND $2::int>0`,
			} {
				tag, err := tx.Exec(ctx, query, parents, limit)
				if err != nil {
					return err
				}
				deleted += tag.RowsAffected()
			}
		}
		tag, err := tx.Exec(ctx, `WITH expired AS(SELECT e.id FROM v3_audit.events e WHERE e.created_at<$1 AND e.event_type NOT IN(1,3,6) AND NOT `+pendingRequest("e")+`
		 AND NOT EXISTS(SELECT 1 FROM v3_audit.request_audits r WHERE r.request_id=e.request_id AND (`+terminalAudit("r")+`) IS NOT TRUE) ORDER BY e.created_at,e.id LIMIT $2 FOR UPDATE OF e SKIP LOCKED)
		 DELETE FROM v3_audit.events e USING expired x WHERE e.id=x.id`, cutoff, limit)
		if err != nil {
			return err
		}
		deleted += tag.RowsAffected()
		// Null timestamps are retained. Invalid timestamps abort the batch;
		// imported orphans passed strict timestamp validation before reaching here.
		tag, err = tx.Exec(ctx, `WITH expired AS(SELECT o.attempt_id FROM v3_audit.orphan_request_attempt_history o WHERE
		 (CASE WHEN jsonb_typeof(o.source_record->'created_at')='number' THEN to_timestamp((o.source_record->>'created_at')::double precision) ELSE (o.source_record->>'created_at')::timestamptz END)<$1
		 AND (CASE WHEN jsonb_typeof(o.source_record->'started_at')='number' THEN to_timestamp((o.source_record->>'started_at')::double precision) ELSE (o.source_record->>'started_at')::timestamptz END)<$1
		 AND (CASE WHEN jsonb_typeof(o.source_record->'completed_at')='number' THEN to_timestamp((o.source_record->>'completed_at')::double precision) ELSE (o.source_record->>'completed_at')::timestamptz END) BETWEEN '1970-01-01'::timestamptz+interval '1 microsecond' AND $1-interval '1 microsecond'
		 AND o.source_record->>'status' IN('succeeded','failed','canceled','cancelled','completed') AND NOT `+pendingRequest("o")+`
		 AND NOT EXISTS(SELECT 1 FROM v3_audit.request_audits r WHERE r.request_id=o.request_id AND (r.created_at IS NULL OR r.started_at IS NULL OR r.created_at>=$1 OR r.started_at>=$1 OR r.completed_at>=$1 OR (`+terminalAudit("r")+`) IS NOT TRUE))
		 ORDER BY o.attempt_id LIMIT $2 FOR UPDATE OF o SKIP LOCKED)
		 DELETE FROM v3_audit.orphan_request_attempt_history o USING expired x WHERE o.attempt_id=x.attempt_id`, cutoff, limit)
		if err != nil {
			return err
		}
		deleted += tag.RowsAffected()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// Empty, fully expired monthly partitions can return their files to the OS.
// The boundary month and DEFAULT retain protected/recent rows. Lock waits are
// bounded, and DROP's normal dependency checks remain enabled.
func ReclaimExpiredUsagePartitions(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, cutoff time.Time) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout='200ms'; SET LOCAL statement_timeout='5s'"); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE i.inhparent='v3_billing.usage_logs'::regclass AND n.nspname='v3_billing' AND c.relname ~ '^usage_logs_[0-9]{6}$' ORDER BY c.relname`)
		if err != nil {
			return err
		}
		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			names = append(names, name)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, name := range names {
			month, err := time.Parse("200601", name[len("usage_logs_"):])
			if err != nil {
				return err
			}
			if month.AddDate(0, 1, 0).After(cutoff.UTC()) {
				continue
			}
			qualified := pgx.Identifier{"v3_billing", name}.Sanitize()
			// A misleading name must never cause a current/future partition to drop.
			var bound string
			if err := tx.QueryRow(ctx, "SELECT pg_get_expr(relpartbound,oid) FROM pg_class WHERE oid=$1::regclass", qualified).Scan(&bound); err != nil {
				return err
			}
			var lower, upper time.Time
			if err := tx.QueryRow(ctx, "SELECT split_part($1,chr(39),2)::timestamptz,split_part($1,chr(39),4)::timestamptz", bound).Scan(&lower, &upper); err != nil {
				return err
			}
			if !lower.Equal(month) || !upper.Equal(month.AddDate(0, 1, 0)) {
				return fmt.Errorf("audit: usage partition bounds differ from managed month")
			}
			if _, err := tx.Exec(ctx, "LOCK TABLE "+qualified+" IN ACCESS EXCLUSIVE MODE"); err != nil {
				return err
			}
			var populated bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+qualified+")").Scan(&populated); err != nil {
				return err
			}
			if !populated {
				if _, err := tx.Exec(ctx, "DROP TABLE "+qualified); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
