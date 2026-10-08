package incentives

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type referralQualification struct {
	id, order, inviter, invitee, rate, share, max, reserved, paid, reversed, ancillary int64
	delay                                                                              int
	state, reason                                                                      string
	paidAt, until                                                                      *time.Time
}

func scanQualification(row pgx.Row) (referralQualification, error) {
	var q referralQualification
	err := row.Scan(&q.id, &q.order, &q.inviter, &q.invitee, &q.rate, &q.share, &q.max, &q.reserved, &q.paid, &q.reversed, &q.delay, &q.state, &q.reason, &q.paidAt, &q.until, &q.ancillary)
	return q, err
}

const qualificationColumns = `id,order_id,inviter_id,invitee_id,reward_ppm,profit_share_ppm,max_reward_credits,reserved_credits,paid_credits,refunded_reward_credits,delay_days,state,reason,paid_at,window_until,ancillary_cost_ppm`

func referralAward(revenue, cost, rate, share, cap int64) (int64, error) {
	if revenue < 0 || cost < 0 || rate < 0 || share < 0 || cap < 0 {
		return 0, ErrInvalid
	}
	scaled := func(value, ppm int64) *big.Int {
		return new(big.Int).Quo(new(big.Int).Mul(big.NewInt(value), big.NewInt(ppm)), big.NewInt(1000000))
	}
	a, b := scaled(revenue, rate), scaled(max(revenue-cost, 0), share)
	a = minBig(a, b)
	a = minBig(a, big.NewInt(cap))
	if !a.IsInt64() {
		return 0, credits.ErrOverflow
	}
	return a.Int64(), nil
}
func minBig(a, b *big.Int) *big.Int {
	if a.Cmp(b) > 0 {
		return b
	}
	return a
}

// SettleReferrals settles all matured known-cost facts. Frozen promises keep
// their remaining reservation after window close so late recovered usage can pay.
func (s *Service) SettleReferrals(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if err := s.releaseInactiveReferralReservations(ctx, limit); err != nil {
		return 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM v3_commerce.referral_consumption_qualifications WHERE state IN('active','completed') OR (state='needs_review' AND reason='cost_unknown') ORDER BY updated_at,id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	count := 0
	var failures []error
	for _, id := range ids {
		if err = s.settleReferral(ctx, id); err != nil {
			failures = append(failures, fmt.Errorf("referral qualification %d: %w", id, err))
			if _, touchErr := s.pool.Exec(ctx, `UPDATE v3_commerce.referral_consumption_qualifications SET updated_at=$2 WHERE id=$1`, id, s.now()); touchErr != nil {
				failures = append(failures, touchErr)
			}
			continue
		}
		count++
	}
	return count, errors.Join(failures...)
}
func (s *Service) settleReferral(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var invitee, order int64
		if err := tx.QueryRow(ctx, `SELECT invitee_id,order_id FROM v3_commerce.referral_consumption_qualifications WHERE id=$1`, id).Scan(&invitee, &order); err != nil {
			return err
		}
		var refunded bool
		if err := tx.QueryRow(ctx, `SELECT state='refunded' FROM v3_commerce.orders WHERE id=$1 FOR SHARE`, order).Scan(&refunded); err != nil {
			return err
		}
		if err := referralLock(ctx, tx, invitee); err != nil {
			return err
		}
		q, err := scanQualification(tx.QueryRow(ctx, `SELECT `+qualificationColumns+` FROM v3_commerce.referral_consumption_qualifications WHERE id=$1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if q.state != "active" && q.state != "completed" && (q.state != "needs_review" || q.reason != "cost_unknown") {
			return nil
		}
		if q.paidAt == nil || q.until == nil {
			return ErrReferralConflict
		}
		var revenue, cost int64
		var known bool
		if !refunded {
			revenue, cost, known, err = referralFacts(ctx, tx, q, s.now().Add(-time.Duration(q.delay)*24*time.Hour))
			if err != nil {
				return err
			}
			if !known {
				_, err = tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_qualifications SET state='needs_review',reason='cost_unknown',updated_at=$2 WHERE id=$1`, q.id, s.now())
				return err
			}
		}
		target, err := referralAward(revenue, cost, q.rate, q.share, q.max)
		if err != nil {
			return err
		}
		net := q.paid - q.reversed
		if target < net {
			if err = s.offsetRefundTx(ctx, tx, q, net-target); err != nil {
				return err
			}
			q.reversed += net - target
			net = target
		}
		if target-net > q.reserved {
			_, err = tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_qualifications SET state='needs_review',reason='refund_reinstatement_budget',refunded_reward_credits=$2,net_revenue_credits=$3,net_cost_credits=$4,updated_at=$5 WHERE id=$1`, q.id, q.reversed, revenue, cost, s.now())
			return err
		}
		delta := max(target-net, 0)
		if delta > 0 {
			if err = s.payReferralTx(ctx, tx, q, delta); err != nil {
				return err
			}
			q.paid += delta
			q.reserved -= delta
		}
		state := "active"
		if refunded {
			state = "refunded"
		} else if !s.now().Before(q.until.Add(time.Duration(q.delay) * 24 * time.Hour)) {
			state = "completed"
		}
		if refunded && q.reserved > 0 {
			if _, err = tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_policy SET reserved_credits=reserved_credits-$1 WHERE id`, q.reserved); err != nil {
				return err
			}
			q.reserved = 0
		}
		_, err = tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_qualifications SET state=$2,reason='',paid_credits=$3,refunded_reward_credits=$4,reserved_credits=$5,net_revenue_credits=$6,net_cost_credits=$7,updated_at=$8 WHERE id=$1`, q.id, state, q.paid, q.reversed, q.reserved, revenue, cost, s.now())
		return err
	})
}

func (s *Service) offsetRefundTx(ctx context.Context, tx pgx.Tx, q referralQualification, amount int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO v3_commerce.referral_consumption_offsets(user_id,debt_credits,updated_at) VALUES($1,$2,$3)
 ON CONFLICT(user_id) DO UPDATE SET debt_credits=v3_commerce.referral_consumption_offsets.debt_credits+EXCLUDED.debt_credits,updated_at=EXCLUDED.updated_at`, q.inviter, amount, s.now())
	return err
}
func (s *Service) payReferralTx(ctx context.Context, tx pgx.Tx, q referralQualification, amount int64) error {
	if s.poster == nil {
		return ErrUnavailable
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_commerce.referral_consumption_offsets(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, q.inviter); err != nil {
		return err
	}
	var debt int64
	if err := tx.QueryRow(ctx, `SELECT debt_credits FROM v3_commerce.referral_consumption_offsets WHERE user_id=$1 FOR UPDATE`, q.inviter).Scan(&debt); err != nil {
		return err
	}
	offset := min(debt, amount)
	award := amount - offset
	operation := fmt.Sprintf("referral-consumption:%d:%d", q.id, q.paid+amount)
	var entry *int64
	if award > 0 {
		var wallet int64
		err := tx.QueryRow(ctx, `INSERT INTO v3_billing.accounts(owner_type,owner_id,kind) VALUES('user',$1,'wallet') ON CONFLICT(owner_type,owner_id,kind) DO UPDATE SET owner_id=EXCLUDED.owner_id RETURNING id`, q.inviter).Scan(&wallet)
		if err != nil {
			return err
		}
		result, err := s.poster.PostTx(ctx, tx, billing.Entry{AccountID: wallet, Amount: credits.Micro(award), Kind: "reward", OperationID: operation, Reason: "referral_reward", Metadata: map[string]any{"source": "referral_reward", "qualification_id": q.id, "non_transferable": true, "non_refundable": true}})
		if err != nil {
			return err
		}
		entry = &result.EntryID
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_commerce.referral_consumption_payouts(qualification_id,operation_id,amount_credits,offset_credits,ledger_entry_id,created_at) VALUES($1,$2,$3,$4,$5,$6)`, q.id, operation, amount, offset, entry, s.now()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_offsets SET debt_credits=debt_credits-$2,updated_at=$3 WHERE user_id=$1`, q.inviter, offset, s.now()); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE v3_commerce.referral_consumption_policy SET reserved_credits=reserved_credits-$1,spent_credits=spent_credits+$2,updated_at=$3 WHERE id`, amount, award, s.now())
	return err
}
