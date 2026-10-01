package ledger

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

func insertUsageLogs(ctx context.Context, tx pgx.Tx, events []event) error {
	rows := make([][]any, 0, len(events))
	for _, e := range events {
		// Sweep events are bookkeeping, not user usage. Historical fixtures can
		// omit the model and still be posted into the ledger.
		if e.fields[billing.FieldModel] == "" || e.fields["funding_part"] == "secondary" {
			continue
		}
		number := func(key string) int64 { n, _ := strconv.ParseInt(e.fields[key], 10, 64); return max(n, 0) }
		ms := number(billing.FieldTimestamp)
		if ms == 0 {
			stamp, _, _ := strings.Cut(e.streamID, "-")
			ms, _ = strconv.ParseInt(stamp, 10, 64)
		}
		amount := e.amount
		if e.fields["funding_part"] == "primary" {
			amount = number("usage_total_amount")
		}
		rows = append(rows, []any{time.UnixMilli(ms).UTC(), e.accountID, number(billing.FieldUserID), number(billing.FieldKeyID),
			number(billing.FieldChannelID), number(billing.FieldCredentialID), amount, number(billing.FieldPromptTokens),
			number(billing.FieldOutputTokens), number(billing.FieldCachedTokens), e.fields[billing.FieldEstimated] == "1",
			e.requestID, e.fields[billing.FieldModel], e.fields[billing.FieldTerminal]})
	}
	if len(rows) == 0 {
		return nil
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"v3_billing", "usage_logs"},
		[]string{"created_at", "account_id", "user_id", "key_id", "channel_id", "credential_id", "amount", "prompt_tokens", "completion_tokens", "cached_tokens", "estimated", "request_id", "model", "terminal"},
		pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("ledger: copy usage logs: %w", err)
	}
	return nil
}

// EnsureUsagePartitions creates this and next month's partitions. A historical
// event in DEFAULT is moved into a new month under a short parent-table lock,
// so adding a partition never fails because DEFAULT already contains its rows.
func EnsureUsagePartitions(ctx context.Context, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, now time.Time) error {
	for offset := 0; offset < 2; offset++ {
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, offset, 0)
		end := start.AddDate(0, 1, 0)
		name := "usage_logs_" + start.Format("200601")
		err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('v3:usage_partitions', 0))`); err != nil {
				return err
			}
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, "v3_billing."+name).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return nil
			}
			if _, err := tx.Exec(ctx, `LOCK TABLE v3_billing.usage_logs IN ACCESS EXCLUSIVE MODE`); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `CREATE TEMP TABLE moved_usage ON COMMIT DROP AS SELECT * FROM v3_billing.usage_logs_default WHERE created_at >= $1 AND created_at < $2`, start, end); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM v3_billing.usage_logs_default WHERE created_at >= $1 AND created_at < $2`, start, end); err != nil {
				return err
			}
			sql := fmt.Sprintf(`CREATE TABLE v3_billing.%s PARTITION OF v3_billing.usage_logs FOR VALUES FROM ('%s') TO ('%s')`,
				name, start.Format(time.RFC3339), end.Format(time.RFC3339))
			if _, err := tx.Exec(ctx, sql); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO v3_billing.usage_logs OVERRIDING SYSTEM VALUE SELECT * FROM moved_usage`)
			return err
		})
		if err != nil {
			return fmt.Errorf("ledger: create month partition: %w", err)
		}
	}
	return nil
}
