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
		return fmt.Errorf("invalid historical timestamp")
	}
	t.Time = parsed
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
