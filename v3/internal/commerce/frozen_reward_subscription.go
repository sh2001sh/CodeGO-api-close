package commerce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/marketplace"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// FreezeRewardPlanTx freezes a server-side draft specification. New blind-box
// batches accept only fixed-credit plans and retain their complete specification.
func (s *Service) FreezeRewardPlanTx(ctx context.Context, tx pgx.Tx, planID int64) (json.RawMessage, error) {
	if tx == nil || planID <= 0 {
		return nil, errors.Join(ErrInvalid, marketplace.ErrInvalidInput)
	}
	p, err := scanPlan(tx.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1 AND enabled FOR SHARE`, planID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.Join(ErrNotFound, marketplace.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	if err = validateFrozenRewardPlan(p); err != nil {
		return nil, errors.Join(err, marketplace.ErrInvalidInput)
	}
	return json.Marshal(p)
}

// GrantFrozenRewardTx fulfills a server-published snapshot even if its catalog
// plan was subsequently edited or disabled. It never merges into an old package
// and never grants multiplier cards or recognized sales revenue.
func (s *Service) GrantFrozenRewardTx(ctx context.Context, tx pgx.Tx, userID int64, snapshot json.RawMessage, operationID string) error {
	if tx == nil || userID <= 0 || operationID == "" || len(operationID) > 200 || s.poster == nil {
		return ErrInvalid
	}
	p, err := decodeFrozenRewardPlan(snapshot)
	if err != nil {
		return err
	}
	var locked int64
	if err = tx.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 FOR UPDATE`, userID).Scan(&locked); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "subscription:reward:"+operationID); err != nil {
		return err
	}
	done, err := checkRewardReceiptTx(ctx, tx, operationID, userID, p.ID)
	if err != nil {
		return err
	}
	if done {
		var prior Plan
		err = tx.QueryRow(ctx, `SELECT plan_snapshot FROM v3_commerce.subscriptions WHERE reward_operation=$1 AND user_id=$2`, operationID, userID).Scan(&prior)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !reflect.DeepEqual(prior, p)) {
			return billing.ErrPostConflict
		}
		return err
	}
	zero := credits.Micro(0)
	err = s.insertNewSubscriptionTx(ctx, tx, Order{
		PolicyVersion: PolicyStandardV2, PlanSnapshot: p, UserID: userID, PlanID: &p.ID,
		Credits: p.Credits, PeriodSeconds: p.PeriodSeconds, ResetPeriod: "never", TradeNo: operationID,
		DurationUnit: p.DurationUnit, DurationValue: p.DurationValue, CustomSeconds: p.CustomSeconds,
		RecognizedRevenueCredits: &zero,
	})
	if err != nil {
		return err
	}
	var id int64
	if err = tx.QueryRow(ctx, `SELECT id FROM v3_commerce.subscriptions WHERE reward_operation=$1`, operationID).Scan(&id); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_commerce.subscription_reward_receipts(operation_id,user_id,plan_id,subscription_id,credits,monthly_seconds)
	 VALUES($1,$2,$3,$4,$5,0)`, operationID, userID, p.ID, id, int64(p.Credits))
	return err
}

func decodeFrozenRewardPlan(snapshot json.RawMessage) (Plan, error) {
	var p Plan
	if len(snapshot) == 0 || len(snapshot) > 64*1024 {
		return p, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(snapshot))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, ErrInvalid
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return p, ErrInvalid
	}
	return p, validateFrozenRewardPlan(p)
}

func validateFrozenRewardPlan(p Plan) error {
	if p.ID <= 0 || strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || len(p.UpgradeGroup) > 64 || p.PolicyVersion != PolicyStandardV2 || normalizePlanPolicy(&p) != nil {
		return ErrInvalid
	}
	duration := p
	if normalizePlanDuration(&duration) != nil || duration.PeriodSeconds != p.PeriodSeconds || p.ResetPeriod != "never" {
		return ErrInvalid
	}
	for model, limit := range p.ModelLimits {
		if strings.TrimSpace(model) == "" || len(model) > 200 || limit <= 0 {
			return ErrInvalid
		}
	}
	return nil
}
