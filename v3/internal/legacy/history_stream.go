package legacy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func walkHistory(ctx context.Context, source pgx.Tx, table string, visit func(json.RawMessage) error) error {
	if table == "" {
		return nil
	}
	rows, err := source.Query(ctx, "SELECT to_jsonb(t) FROM "+table+" t")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw json.RawMessage
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		if err = visit(raw); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Classify against the same source snapshot without retaining request IDs in
// memory. Missing historical parents never become invented live audits.
func walkHistoryAttempts(ctx context.Context, source pgx.Tx, attempts, requests string, visit func(json.RawMessage, bool) error) error {
	if attempts == "" {
		return nil
	}
	if requests == "" {
		return fmt.Errorf("legacy: attempt history requires the source request audit table")
	}
	rows, err := source.Query(ctx, `SELECT to_jsonb(a),r.request_id IS NULL FROM `+attempts+` a
	 LEFT JOIN `+requests+` r ON r.request_id=a.request_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw json.RawMessage
		var orphan bool
		if err = rows.Scan(&raw, &orphan); err != nil {
			return err
		}
		if err = visit(raw, orphan); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Duplicate usage request IDs are qualified only within the same timestamp and
// user, matching the target usage uniqueness constraint. PostgreSQL can spill
// the grouping/join to disk; no per-log duplicate map lives in the importer.
// Original JSON is kept separate from the derived flag to avoid modifying or
// shadowing any source field, including the audit event's original request ID.
func walkHistoryLogs(ctx context.Context, source pgx.Tx, table string, visit func(json.RawMessage, bool) error) error {
	if table == "" {
		return nil
	}
	rows, err := source.Query(ctx, `SELECT to_jsonb(l),COALESCE(l.type=2 AND repeated.request_id IS NOT NULL,false)
	 FROM `+table+` l LEFT JOIN
	 (SELECT created_at,request_id,user_id FROM `+table+` WHERE type=2 AND request_id<>''
	 GROUP BY created_at,request_id,user_id HAVING count(*)>1) repeated
	 ON l.created_at=repeated.created_at AND l.request_id=repeated.request_id AND l.user_id=repeated.user_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw json.RawMessage
		var duplicate bool
		if err = rows.Scan(&raw, &duplicate); err != nil {
			return err
		}
		if err = visit(raw, duplicate); err != nil {
			return err
		}
	}
	return rows.Err()
}

// GORM timestamps are timestamptz, while old log timestamps are Unix seconds.
type historyTime struct{ time.Time }

func (t *historyTime) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var seconds int64
	if json.Unmarshal(b, &seconds) == nil {
		t.Time = time.Unix(seconds, 0).UTC()
		return nil
	}
	var encoded string
	if json.Unmarshal(b, &encoded) != nil {
		return fmt.Errorf("invalid historical timestamp")
	}
	parsed, err := time.Parse(time.RFC3339Nano, encoded)
	if err != nil {
		// PostgreSQL emits historical local-mean-time offsets with seconds,
		// including Go's year-1 unset timestamps in Asia/Shanghai. Keep the
		// instant rather than rejecting valid source timestamptz values.
		parsed, err = time.Parse("2006-01-02T15:04:05.999999999-07:00:00", encoded)
		if err == nil {
			// time.Parse permits 60 in zone minutes/seconds and hour 24.
			// PostgreSQL's second-resolution offsets use normalized fields.
			offset := encoded[len(encoded)-9:]
			if offset[1:3] > "23" || offset[4:6] > "59" || offset[7:9] > "59" {
				return fmt.Errorf("invalid historical timestamp")
			}
		}
	}
	if err != nil {
		return fmt.Errorf("invalid historical timestamp")
	}
	// Go's JSON timestamp formatter only writes minute-resolution offsets.
	// Normalize before import/check serialization to avoid losing seconds.
	t.Time = parsed.UTC()
	return nil
}

func historyDate(t historyTime) time.Time {
	if t.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return t.Time
}

func historyMetadata(b json.RawMessage) json.RawMessage {
	if len(b) == 0 || string(b) == "null" {
		return json.RawMessage(`{}`)
	}
	return b
}
