package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type IssueRedemptionInput struct {
	PlanSnapshot     Plan          `json:"-"`
	Name             string        `json:"name"`
	Credits          credits.Micro `json:"credits"`
	ExpiresAt        *time.Time    `json:"expires_at"`
	RedeemType       string        `json:"redeem_type,omitempty"`
	PlanID           int64         `json:"plan_id,omitempty"`
	BlindBoxQuantity int           `json:"blind_box_quantity,omitempty"`
	PlanTitle        string        `json:"-"`
}

type RedemptionResult struct {
	RedeemType         string        `json:"redeem_type"`
	Credits            credits.Micro `json:"credits"`
	PlanID             int64         `json:"plan_id,omitempty"`
	PlanTitle          string        `json:"plan_title,omitempty"`
	UserSubscriptionID int64         `json:"user_subscription_id,omitempty"`
	BlindBoxQuantity   int           `json:"blind_box_quantity,omitempty"`
	BlindBoxOrderID    int64         `json:"blind_box_order_id,omitempty"`
}

func (s *Service) validateRedemptionInput(ctx context.Context, in *IssueRedemptionInput) error {
	if len(in.Name) > 200 || in.Credits < 0 || (in.ExpiresAt != nil && !in.ExpiresAt.After(s.cfg.Now())) {
		return ErrInvalid
	}
	if in.RedeemType == "" {
		in.RedeemType = "credits"
	}
	switch in.RedeemType {
	case "credits":
		if in.Credits <= 0 || in.PlanID != 0 || in.BlindBoxQuantity != 0 {
			return ErrInvalid
		}
	case "subscription":
		if in.PlanID <= 0 || in.Credits != 0 || in.BlindBoxQuantity != 0 {
			return ErrInvalid
		}
		p, err := scanPlan(s.pool.QueryRow(ctx, `SELECT `+planColumns+` FROM v3_commerce.plans WHERE id=$1`, in.PlanID))
		in.PlanSnapshot, in.PlanTitle = p, p.Name
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	case "blind_box":
		if in.BlindBoxQuantity < 1 || in.BlindBoxQuantity > 100 || in.Credits != 0 || in.PlanID != 0 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func decodeRedemptionResult(saved []byte, result *RedemptionResult) error {
	if len(saved) == 0 {
		return nil
	} // Imported used codes remain consumed; never issue their benefits again.
	return json.Unmarshal(saved, result)
}
