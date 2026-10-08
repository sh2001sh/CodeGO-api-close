package marketplace

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// DrawBatch purchases and reveals in the same transaction. There is no sold
// unopened inventory competing for a later version's remaining prizes.
func (s *Service) DrawBatch(ctx context.Context, user, batch int64, request string, count int) (BatchDrawResult, error) {
	var out BatchDrawResult
	if validRequest(user, request, count) != nil || batch <= 0 {
		return out, ErrInvalidInput
	}
	account, err := s.wallets.WalletAccount(ctx, user)
	if err != nil {
		return out, err
	}
	input := struct {
		Batch int64
		Count int
	}{batch, count}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockUser(ctx, tx, user); err != nil {
			return err
		}
		if found, err := replay(ctx, tx, user, "batch-draw", request, input, &out); found || err != nil {
			return err
		}
		b, err := scanBatch(tx.QueryRow(ctx, `SELECT `+batchColumns+` FROM v3_marketplace.blind_box_batches WHERE id=$1 FOR UPDATE`, batch))
		if err != nil {
			return err
		}
		if b.State != "published" && (b.State != "paused" || b.Purpose != "consumption") {
			return ErrConflict
		}
		if b.RemainingCount < int64(count) {
			return ErrInventory
		}
		if b.Price > 0 {
			purchased, err := s.batchDailyPurchasedTx(ctx, tx, user)
			if err != nil {
				return err
			}
			if purchased > paidRandomDailyLimit || int64(count) > paidRandomDailyLimit-purchased {
				return ErrDailyLimit
			}
		}
		if b.Purpose == "paid_random" {
			out.Pity, err = lockBatchPityTx(ctx, tx, user)
			if err != nil {
				return err
			}
		} else {
			out.Pity, err = readBatchPityTx(ctx, tx, user)
			if err != nil {
				return err
			}
		}
		if b.Purpose == "consumption" {
			tag, err := tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_batch_entitlements SET available_count=available_count-$3 WHERE batch_id=$1 AND user_id=$2 AND available_count>=$3`, batch, user, count)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrInventory
			}
			b.EntitledCount -= int64(count)
		}
		var revenueAccount int64
		if b.Price > 0 {
			if _, err = tx.Exec(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('platform',1,'platform_revenue') ON CONFLICT(owner_type,owner_id,kind) DO NOTHING`); err != nil {
				return err
			}
			if err = tx.QueryRow(ctx, `SELECT id FROM v3_billing.accounts WHERE owner_type='platform' AND owner_id=1 AND kind='platform_revenue'`).Scan(&revenueAccount); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `SELECT id FROM v3_billing.accounts WHERE id=ANY($1) ORDER BY id FOR UPDATE`, []int64{account, b.EscrowAccountID, revenueAccount}); err != nil {
			return err
		}
		out.BatchID = batch
		out.Records = make([]OpenRecord, 0, count)
		// Publication validates full-batch purchase and liability multiplication.
		out.Charged = b.Price * credits.Micro(count)
		out.BaseCredits = b.BaseCredits * credits.Micro(count)
		meta := map[string]any{"batch_id": batch, "non_transferable": true, "non_refundable": true}
		key := operation("batch", user, request, 0)
		if out.Charged > 0 {
			if _, err = s.money.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -out.Charged, Kind: "transfer", OperationID: key + ":purchase", Reason: "blind_box_batch_purchase", Metadata: meta}); err != nil {
				return err
			}
			// Internal wallet movement is not new recognized sales revenue.
			if _, err = s.money.PostTx(ctx, tx, billing.Entry{AccountID: revenueAccount, Amount: out.Charged, Kind: "transfer", OperationID: key + ":proceeds", Reason: "blind_box_batch_wallet_proceeds", Metadata: meta}); err != nil {
				return err
			}
		}
		var liability credits.Micro
		for i := 0; i < count; i++ {
			record, err := drawBatchRecord(&b, &out.Pity, s.cfg.Draw)
			if err != nil {
				return err
			}
			r := record.Reward
			liability, err = liability.Add(r.Amount)
			if err != nil {
				return err
			}
			liability, err = liability.Add(record.GuaranteeCredits)
			if err != nil {
				return err
			}
			record.CreatedAt = s.cfg.Now()
			payload, err := json.Marshal(record.Reward)
			if err != nil {
				return err
			}
			recordKey := fmt.Sprintf("batch:%s:%d", request, i)
			if err = tx.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_open_records(user_id,request_id,reward,created_at,guarantee_type,batch_id,pool_type,reward_wallet_type,guarantee_credits_micro) VALUES($1,$2,$3,$4,$6,$5,'batch','api_only',$7) RETURNING id`, user, recordKey, payload, record.CreatedAt, batch, record.Guarantee, record.GuaranteeCredits).Scan(&record.ID); err != nil {
				return err
			}
			grantKey := operation("batch", user, request, i)
			if r.Kind == "credits" {
				if _, err = s.money.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: r.Amount, Kind: "reward", OperationID: grantKey + ":reward", Reason: "blind_box_batch_reward", Metadata: meta}); err != nil {
					return err
				}
			} else {
				if len(r.PlanSnapshot) == 0 {
					return ErrConflict
				}
				if err = tx.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_props(user_id,open_record_id,kind,title,plan_id,plan_snapshot) VALUES($1,$2,'subscription',$3,$4,$5) RETURNING id`, user, record.ID, r.Title, r.PlanID, r.PlanSnapshot).Scan(&record.PropID); err != nil {
					return err
				}
			}
			if record.GuaranteeCredits > 0 {
				if _, err = s.money.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: record.GuaranteeCredits, Kind: "reward", OperationID: grantKey + ":guarantee", Reason: "blind_box_batch_reward", Metadata: meta}); err != nil {
					return err
				}
			}
			out.Records = append(out.Records, record)
		}
		liability, err = liability.Add(out.BaseCredits)
		if err != nil {
			return err
		}
		if liability > b.RemainingBudget {
			return ErrInventory
		}
		if _, err = s.money.PostTx(ctx, tx, billing.Entry{AccountID: b.EscrowAccountID, Amount: -liability, Kind: "transfer", OperationID: key + ":budget", Reason: "blind_box_batch_fulfilment", Metadata: meta}); err != nil {
			return err
		}
		if out.BaseCredits > 0 {
			if _, err = s.money.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: out.BaseCredits, Kind: "reward", OperationID: key + ":base", Reason: "blind_box_batch_base", Metadata: meta}); err != nil {
				return err
			}
		}
		b.SpentBudget += liability
		if b.RemainingCount == 0 {
			b.State = "exhausted"
		}
		payload, err := json.Marshal(b.Rewards)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_batches SET state=$2,remaining_count=$3,entitled_count=$4,spent_budget_micro=$5,rewards=$6,updated_at=$7 WHERE id=$1`, batch, b.State, b.RemainingCount, b.EntitledCount, b.SpentBudget, payload, s.cfg.Now()); err != nil {
			return err
		}
		if b.Purpose == "paid_random" {
			if _, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_batch_pity SET opened=$2,small_progress=$3,big_progress=$4 WHERE user_id=$1`, user, out.Pity.Opened, out.Pity.SmallProgress, out.Pity.BigProgress); err != nil {
				return err
			}
		}
		return remember(ctx, tx, user, "batch-draw", request, input, out)
	})
	if err != nil {
		return BatchDrawResult{}, err
	}
	return out, nil
}
