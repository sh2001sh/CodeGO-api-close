package incentives

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

func (s *Service) settleReward(ctx context.Context, id int64) error {
	if s.poster == nil {
		return ErrUnavailable
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var user, draw int64
		var amount credits.Micro
		var status, date string
		err := tx.QueryRow(ctx, `SELECT r.user_id,r.draw_id,r.final_reward_credits,r.credit_status,d.draw_date FROM v3_commerce.subscription_lucky_rewards r JOIN v3_commerce.subscription_lucky_draws d ON d.id=r.draw_id WHERE r.id=$1 FOR UPDATE OF r`, id).Scan(&user, &draw, &amount, &status, &date)
		if err != nil {
			return err
		}
		if status == "credited" {
			return nil
		}
		if amount > 0 {
			var account int64
			err = tx.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',$1,'wallet') ON CONFLICT(owner_type,owner_id,kind) DO UPDATE SET owner_id=EXCLUDED.owner_id RETURNING id`, user).Scan(&account)
			if err != nil {
				return err
			}
			if _, err = s.poster.PostTx(ctx, tx, billing.Entry{AccountID: account, Amount: amount, Kind: "reward", OperationID: fmt.Sprintf("lucky-draw:%s:reward:%d", date, id), Reason: "subscription_lucky_draw", Metadata: map[string]any{"user_id": user, "draw_id": draw, "reward_id": id}}); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_lucky_reward_notifications(reward_id,user_id) VALUES($1,$2) ON CONFLICT(reward_id) DO NOTHING`, id, user); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.subscription_lucky_rewards SET credit_status='credited',credit_error='',credited_at=$2,updated_at=$2 WHERE id=$1`, id, s.now())
		return err
	})
}
