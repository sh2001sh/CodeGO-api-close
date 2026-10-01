package ledger

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// deadLetter is an event the ledger cannot post.
type deadLetter struct {
	streamID string
	reason   string
	fields   map[string]string
}

// dropUnknownAccounts removes charges for accounts with no row and turns them
// into dead letters. Their dedup rows are deleted so a repaired account can
// be re-posted from the dead letter later.
func dropUnknownAccounts(ctx context.Context, tx pgx.Tx, charges []event, bad *[]deadLetter) ([]event, error) {
	if len(charges) == 0 {
		return charges, nil
	}
	ids := accountIDs(charges)
	rows, err := tx.Query(ctx, `SELECT id FROM v3_billing.accounts WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("ledger: check accounts: %w", err)
	}
	known := make(map[int64]bool, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		known[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	kept := charges[:0]
	for _, e := range charges {
		if known[e.accountID] {
			kept = append(kept, e)
			continue
		}
		*bad = append(*bad, deadLetter{streamID: e.streamID, reason: "unknown account", fields: e.fields})
		if _, err := tx.Exec(ctx, `DELETE FROM v3_billing.billing_dedup WHERE request_id = $1 AND account_id = $2`,
			e.requestID, e.accountID); err != nil {
			return nil, fmt.Errorf("ledger: release dedup %s: %w", e.requestID, err)
		}
	}
	return kept, nil
}

func insertDeadLetters(ctx context.Context, tx pgx.Tx, bad []deadLetter) error {
	for _, d := range bad {
		payload, _ := json.Marshal(d.fields)
		if _, err := tx.Exec(ctx, `INSERT INTO v3_billing.dead_letters (stream_id, reason, payload) VALUES ($1, $2, $3)`,
			d.streamID, d.reason, payload); err != nil {
			return fmt.Errorf("ledger: dead letter %s: %w", d.streamID, err)
		}
	}
	return nil
}
