package legacy

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// A child update can bring an old parent into the window, expire its last
// retained child, or move a child between parents. Replay both old and new
// parents and their current children from the same snapshot before acknowledging.
func onlineRetentionParents(ctx context.Context, source, target pgx.Tx, sources map[string]string, keys map[string][]json.RawMessage) error {
	children := keys["request_attempt_audits"]
	if historyCutoffFrom(ctx).IsZero() || len(children) == 0 || sources["request_audits"] == "" {
		return nil
	}
	data, err := json.Marshal(children)
	if err != nil {
		return err
	}
	parents := map[string]json.RawMessage{}
	bytes := 0
	for _, key := range keys["request_audits"] {
		var identity struct {
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(key, &identity); err != nil || identity.RequestID == "" {
			return errors.New("legacy: invalid retained parent key")
		}
		canonical, err := json.Marshal(identity)
		if err != nil {
			return err
		}
		if _, exists := parents[identity.RequestID]; !exists {
			parents[identity.RequestID] = canonical
			bytes += len(canonical)
		}
	}
	collect := func(tx pgx.Tx, query string) error {
		rows, err := tx.Query(ctx, query, data)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			key, err := json.Marshal(map[string]string{"request_id": id})
			if err != nil {
				return err
			}
			if _, exists := parents[id]; !exists {
				bytes += len(key)
				if bytes > exactBulkBytes {
					return errors.New("legacy: retained parent replay exceeds bounded batch")
				}
				parents[id] = key
			}
		}
		return rows.Err()
	}
	if err := collect(source, "SELECT DISTINCT a.request_id FROM "+sources["request_attempt_audits"]+" a JOIN jsonb_array_elements($1::jsonb) k ON a.attempt_id=k->>'attempt_id'"); err != nil {
		return err
	}
	old := "SELECT a.request_id FROM " + onlineStage("v3_audit.request_attempt_audits") + " a JOIN jsonb_array_elements($1::jsonb) k ON a.attempt_id=k->>'attempt_id' UNION SELECT a.request_id FROM " + onlineStage("v3_audit.orphan_request_attempt_history") + " a JOIN jsonb_array_elements($1::jsonb) k ON a.attempt_id=k->>'attempt_id'"
	if err := collect(target, old); err != nil {
		return err
	}
	keys["request_audits"] = nil
	for _, key := range parents {
		keys["request_audits"] = append(keys["request_audits"], key)
	}
	return nil
}
