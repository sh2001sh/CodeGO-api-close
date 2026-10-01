package legacy

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// The old reward game stays in the source backup. Its lifetime usage guard is
// still required by current conversion, renewal pricing and refund policies.
func (d *commerceData) loadResetUsage(ctx context.Context, source pgx.Tx, sources map[string]string) error {
	rows, err := commerceRenewableRows(ctx, source, sources["subscription_reset_opportunity_ledgers"])
	if err != nil {
		return err
	}
	d.resetUsed, err = commerceResetUsed(rows)
	return err
}

func commerceResetUsed(rows []commerceRow) (map[int64]bool, error) {
	used := map[int64]bool{}
	for _, row := range rows {
		change, err := row.text("change_type")
		if err != nil {
			return nil, err
		}
		if change != "use" {
			continue
		}
		id, err := row.integer("related_user_id")
		if err != nil {
			return nil, err
		}
		if id > 0 {
			used[id] = true
		}
	}
	return used, nil
}
