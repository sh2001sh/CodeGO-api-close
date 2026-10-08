package channelmarket

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// All settlements are already locked. Resolve and lock the entire financial
// account set before the first transfer, matching usage's global account order.
func prelockIncomeReleaseAccountsTx(ctx context.Context, tx pgx.Tx, items []releasableSettlement) error {
	if len(items) == 0 {
		return nil
	}
	owners := make([]int64, len(items))
	for i, item := range items {
		owners[i] = item.owner
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind)
	 SELECT 'user',owner_id,kind FROM (SELECT DISTINCT owner_id FROM unnest($1::bigint[]) owner_id) owners
	 CROSS JOIN (VALUES('wallet'),('marketplace_pending')) kinds(kind) ORDER BY owner_id,kind
	 ON CONFLICT(owner_type,owner_id,kind) DO NOTHING`, owners); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id,owner_id,kind FROM v3_billing.accounts
	 WHERE owner_type='user' AND owner_id=ANY($1) AND kind IN ('wallet','marketplace_pending')`, owners)
	if err != nil {
		return err
	}
	wallets, pending := make(map[int64]int64), make(map[int64]int64)
	ids := make([]int64, 0, len(items)*2)
	for rows.Next() {
		var id, owner int64
		var kind string
		if err = rows.Scan(&id, &owner, &kind); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
		if kind == "wallet" {
			wallets[owner] = id
		} else {
			pending[owner] = id
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if err = lockAccounts(ctx, tx, ids...); err != nil {
		return err
	}
	for i := range items {
		items[i].wallet, items[i].pending = wallets[items[i].owner], pending[items[i].owner]
	}
	return nil
}
