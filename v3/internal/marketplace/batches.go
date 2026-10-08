package marketplace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

const batchColumns = `id,revision,name,purpose,state,price_micro,base_credits_micro,budget_micro,required_budget_micro,spent_budget_micro,total_count,remaining_count,entitled_count,ancillary_cost_ppm,contribution_share_ppm,costs_confirmed,rewards,published_at,coalesce(escrow_account_id,0)`

func scanBatch(row pgx.Row) (Batch, error) {
	var b Batch
	var rewards []byte
	err := row.Scan(&b.ID, &b.Revision, &b.Name, &b.Purpose, &b.State, &b.Price, &b.BaseCredits, &b.Budget, &b.RequiredBudget, &b.SpentBudget, &b.TotalCount, &b.RemainingCount, &b.EntitledCount, &b.AncillaryCostPPM, &b.ContributionSharePPM, &b.CostsConfirmed, &rewards, &b.PublishedAt, &b.EscrowAccountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, ErrNotFound
	}
	if err != nil {
		return b, err
	}
	if err = json.Unmarshal(rewards, &b.Rewards); err != nil {
		return b, err
	}
	return b, batchAmounts(&b)
}

func (s *Service) ListBatches(ctx context.Context, admin bool) ([]Batch, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+batchColumns+` FROM v3_marketplace.blind_box_batches WHERE $1 OR state<>'draft' ORDER BY id DESC LIMIT 100`, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Batch, 0)
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (s *Service) BatchOverview(ctx context.Context, user int64) (BatchOverview, error) {
	var out BatchOverview
	if user <= 0 {
		return out, ErrInvalidInput
	}
	var err error
	out.Batches, err = s.ListBatches(ctx, false)
	if err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,batch_id,source,available_count,created_at FROM v3_marketplace.blind_box_batch_entitlements WHERE user_id=$1 AND available_count>0 ORDER BY id`, user)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Entitlements = make([]BatchEntitlement, 0)
	for rows.Next() {
		var e BatchEntitlement
		if err := rows.Scan(&e.ID, &e.BatchID, &e.Source, &e.AvailableCount, &e.CreatedAt); err != nil {
			return out, err
		}
		out.Entitlements = append(out.Entitlements, e)
	}
	return out, rows.Err()
}

func (s *Service) freezeBatchRewardsTx(ctx context.Context, tx pgx.Tx, b *Batch) error {
	for i := range b.Rewards {
		r := &b.Rewards[i]
		r.Remaining = r.Quantity
		r.PlanSnapshot = nil
		if r.Kind == "subscription" {
			port, ok := s.subscriptions.(FrozenSubscriptions)
			if !ok {
				return ErrUnavailable
			}
			snapshot, err := port.FreezeRewardPlanTx(ctx, tx, r.PlanID)
			if err != nil {
				return err
			}
			var spec struct {
				Credits int64 `json:"credits"`
			}
			if err = json.Unmarshal(snapshot, &spec); err != nil || spec.Credits <= 0 {
				return ErrInvalidInput
			}
			r.Amount = credits.Micro(spec.Credits)
			r.PlanSnapshot = snapshot
		}
	}
	b.RemainingCount = 0
	for _, r := range b.Rewards {
		b.RemainingCount += r.Quantity
	}
	return batchAmounts(b)
}

func (s *Service) SaveBatch(ctx context.Context, actor int64, in Batch) (Batch, error) {
	var b Batch
	if err := validateBatch(in); err != nil {
		return b, err
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockUser(ctx, tx, actor); err != nil {
			return err
		}
		if in.ID > 0 {
			old, err := scanBatch(tx.QueryRow(ctx, `SELECT `+batchColumns+` FROM v3_marketplace.blind_box_batches WHERE id=$1 FOR UPDATE`, in.ID))
			if err != nil {
				return err
			}
			if old.State != "draft" || old.Revision != in.Revision {
				return ErrConflict
			}
		} else if in.Revision != 0 {
			return ErrConflict
		}
		b = in
		b.State = "draft"
		b.Revision = in.Revision + 1
		b.SpentBudget, b.EntitledCount, b.EscrowAccountID = 0, 0, 0
		b.PublishedAt = nil
		if err := s.freezeBatchRewardsTx(ctx, tx, &b); err != nil {
			return err
		}
		if b.RequiredBudget > b.Budget {
			return ErrInvalidInput
		}
		payload, err := json.Marshal(b.Rewards)
		if err != nil {
			return err
		}
		if b.ID == 0 {
			return tx.QueryRow(ctx, `INSERT INTO v3_marketplace.blind_box_batches(name,purpose,price_micro,base_credits_micro,budget_micro,required_budget_micro,total_count,remaining_count,ancillary_cost_ppm,contribution_share_ppm,costs_confirmed,rewards) VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,$10,$11) RETURNING id`, b.Name, b.Purpose, b.Price, b.BaseCredits, b.Budget, b.RequiredBudget, b.TotalCount, b.AncillaryCostPPM, b.ContributionSharePPM, b.CostsConfirmed, payload).Scan(&b.ID)
		}
		_, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_batches SET revision=$2,name=$3,purpose=$4,price_micro=$5,base_credits_micro=$6,budget_micro=$7,required_budget_micro=$8,total_count=$9,remaining_count=$9,ancillary_cost_ppm=$10,contribution_share_ppm=$11,costs_confirmed=$12,rewards=$13,updated_at=$14 WHERE id=$1`, b.ID, b.Revision, b.Name, b.Purpose, b.Price, b.BaseCredits, b.Budget, b.RequiredBudget, b.TotalCount, b.AncillaryCostPPM, b.ContributionSharePPM, b.CostsConfirmed, payload, s.cfg.Now())
		return err
	})
	return b, err
}

func (s *Service) ChangeBatchState(ctx context.Context, actor, batch, revision int64, request string, publish bool) (Batch, error) {
	var b Batch
	if err := validRequest(actor, request, 1); err != nil || batch <= 0 || revision <= 0 {
		return b, ErrInvalidInput
	}
	account, err := s.wallets.WalletAccount(ctx, actor)
	if err != nil {
		return b, err
	}
	input := struct {
		Batch, Revision int64
		Publish         bool
	}{batch, revision, publish}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := lockUser(ctx, tx, actor); err != nil {
			return err
		}
		if found, err := replay(ctx, tx, actor, "batch-state", request, input, &b); found || err != nil {
			return err
		}
		var err error
		b, err = scanBatch(tx.QueryRow(ctx, `SELECT `+batchColumns+` FROM v3_marketplace.blind_box_batches WHERE id=$1 FOR UPDATE`, batch))
		if err != nil {
			return err
		}
		if b.Revision != revision {
			return ErrConflict
		}
		action := "pause"
		if publish {
			if b.State != "draft" || !b.CostsConfirmed {
				return ErrConflict
			}
			// The draft preview was frozen server-side on save. Publishing the
			// reviewed revision must not silently substitute changed catalog specs.
			if b.RequiredBudget > b.Budget {
				return ErrInvalidInput
			}
			err = tx.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('platform',$1,'promotion_budget') RETURNING id`, batch).Scan(&b.EscrowAccountID)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `SELECT id FROM v3_billing.accounts WHERE id=ANY($1) ORDER BY id FOR UPDATE`, []int64{account, b.EscrowAccountID}); err != nil {
				return err
			}
			operation := fmt.Sprintf("blind-batch:%d:reserve", batch)
			if _, err = s.money.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: -b.RequiredBudget, Kind: "transfer", OperationID: operation + ":debit", Reason: "blind_box_batch_budget"}); err != nil {
				return err
			}
			if _, err = s.money.PostTx(ctx, tx, billing.Entry{AccountID: b.EscrowAccountID, Amount: b.RequiredBudget, Kind: "transfer", OperationID: operation + ":credit", Reason: "blind_box_batch_budget"}); err != nil {
				return err
			}
			now := s.cfg.Now()
			b.PublishedAt = &now
			b.State, action = "published", "publish"
		} else {
			if b.State != "published" {
				return ErrConflict
			}
			b.State = "paused"
		}
		b.Revision++
		if err = batchAmounts(&b); err != nil {
			return err
		}
		payload, err := json.Marshal(b.Rewards)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE v3_marketplace.blind_box_batches SET revision=$2,state=$3,escrow_account_id=$4,required_budget_micro=$5,total_count=$6,remaining_count=$7,rewards=$8,published_at=$9,updated_at=$10 WHERE id=$1`, batch, b.Revision, b.State, b.EscrowAccountID, b.RequiredBudget, b.TotalCount, b.RemainingCount, payload, b.PublishedAt, s.cfg.Now()); err != nil {
			return err
		}
		snapshot, err := json.Marshal(b)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO v3_marketplace.blind_box_batch_events(batch_id,actor_id,revision,action,snapshot) VALUES($1,$2,$3,$4,$5)`, batch, actor, b.Revision, action, snapshot); err != nil {
			return err
		}
		return remember(ctx, tx, actor, "batch-state", request, input, b)
	})
	return b, err
}
