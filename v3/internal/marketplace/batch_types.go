package marketplace

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type BatchReward struct {
	ID                   string          `json:"id"`
	Title                string          `json:"title"`
	Kind                 string          `json:"kind"`
	Amount               credits.Micro   `json:"amount_micro"`
	PlanID               int64           `json:"plan_id"`
	Quantity             int64           `json:"quantity"`
	Remaining            int64           `json:"remaining"`
	InitialProbability   float64         `json:"initial_probability"`
	RemainingProbability float64         `json:"remaining_probability"`
	PlanSnapshot         json.RawMessage `json:"plan_snapshot,omitempty"`
}
type Batch struct {
	ID                   int64           `json:"id"`
	Revision             int64           `json:"revision"`
	Name                 string          `json:"name"`
	Purpose              string          `json:"purpose"`
	State                string          `json:"state"`
	Price                credits.Micro   `json:"price_micro"`
	BaseCredits          credits.Micro   `json:"base_credits_micro"`
	Budget               credits.Micro   `json:"budget_micro"`
	RequiredBudget       credits.Micro   `json:"required_budget_micro"`
	SpentBudget          credits.Micro   `json:"spent_budget_micro"`
	RemainingBudget      credits.Micro   `json:"remaining_budget_micro"`
	TotalCount           int64           `json:"total_count"`
	RemainingCount       int64           `json:"remaining_count"`
	EntitledCount        int64           `json:"entitled_count"`
	AncillaryCostPPM     int64           `json:"ancillary_cost_ppm"`
	ContributionSharePPM int64           `json:"contribution_share_ppm"`
	CostsConfirmed       bool            `json:"costs_confirmed"`
	Rewards              []BatchReward   `json:"rewards"`
	PityPolicy           BatchPityPolicy `json:"pity_policy"`
	PublishedAt          *time.Time      `json:"published_at,omitempty"`
	EscrowAccountID      int64           `json:"-"`
}
type BatchEntitlement struct {
	ID             int64     `json:"id"`
	BatchID        int64     `json:"batch_id"`
	Source         string    `json:"source"`
	AvailableCount int64     `json:"available_count"`
	CreatedAt      time.Time `json:"created_at"`
}
type BatchOverview struct {
	Batches            []Batch            `json:"batches"`
	Entitlements       []BatchEntitlement `json:"entitlements"`
	DailyPurchaseLimit int                `json:"daily_purchase_limit"`
	DailyPurchased     int64              `json:"daily_purchased"`
	Pity               PityState          `json:"pity"`
}
type BatchDrawResult struct {
	BatchID     int64         `json:"batch_id"`
	BaseCredits credits.Micro `json:"base_credits_micro"`
	Charged     credits.Micro `json:"charged_micro"`
	Records     []OpenRecord  `json:"records"`
	Pity        PityState     `json:"pity"`
}

func validateBatch(b Batch) error {
	if b.ID < 0 || b.Revision < 0 || strings.TrimSpace(b.Name) == "" || len(b.Name) > 200 || b.Budget <= 0 || len(b.Rewards) == 0 || len(b.Rewards) > 100 || b.AncillaryCostPPM < 0 || b.AncillaryCostPPM > 1_000_000 || b.ContributionSharePPM < 1 || b.ContributionSharePPM > 100_000 {
		return ErrInvalidInput
	}
	switch b.Purpose {
	case "consumption":
		if b.Price != 0 || b.BaseCredits != 0 {
			return ErrInvalidInput
		}
	case "credits":
		if b.Price <= 0 || b.BaseCredits < b.Price {
			return ErrInvalidInput
		}
	case "paid_random":
		if b.Price <= 0 || b.Price > credits.Micro(math.MaxInt64/2) || b.BaseCredits != 0 {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	seen := map[string]bool{}
	var quantity int64
	for _, r := range b.Rewards {
		if r.ID == "" || len(r.ID) > 64 || seen[r.ID] || strings.TrimSpace(r.Title) == "" || len(r.Title) > 200 || r.Quantity < 1 || r.Quantity > 1_000_000 || quantity > 1_000_000-r.Quantity {
			return ErrInvalidInput
		}
		seen[r.ID] = true
		quantity += r.Quantity
		switch r.Kind {
		case "credits":
			if r.Amount <= 0 || r.PlanID != 0 {
				return ErrInvalidInput
			}
		case "subscription":
			if r.PlanID <= 0 {
				return ErrInvalidInput
			}
		default:
			return ErrInvalidInput
		}
	}
	return nil
}

func batchAmounts(b *Batch) error {
	var total int64
	var liability credits.Micro
	for i := range b.Rewards {
		r := &b.Rewards[i]
		if r.Amount <= 0 || r.Quantity <= 0 || r.Remaining < 0 || r.Remaining > r.Quantity || r.Amount > credits.Micro(math.MaxInt64/r.Quantity) {
			return credits.ErrOverflow
		}
		var err error
		perReward := r.Amount
		if b.Purpose == "paid_random" {
			if b.Price <= 0 || b.Price > credits.Micro(math.MaxInt64/2) {
				return credits.ErrOverflow
			}
			floor := b.Price * 2
			if r.Kind == "subscription" {
				perReward, err = perReward.Add(floor)
				if err != nil {
					return err
				}
			} else if perReward < floor {
				perReward = floor
			}
		}
		if perReward > credits.Micro(math.MaxInt64/r.Quantity) {
			return credits.ErrOverflow
		}
		liability, err = liability.Add(perReward * credits.Micro(r.Quantity))
		if err != nil {
			return err
		}
		total += r.Quantity
	}
	if total <= 0 || b.BaseCredits > credits.Micro(math.MaxInt64/total) || b.Price > credits.Micro(math.MaxInt64/total) {
		return credits.ErrOverflow
	}
	var err error
	b.RequiredBudget, err = liability.Add(b.BaseCredits * credits.Micro(total))
	if err != nil {
		return err
	}
	b.TotalCount = total
	b.RemainingBudget = b.RequiredBudget - b.SpentBudget
	if b.State == "draft" {
		b.RemainingBudget = 0
	}
	for i := range b.Rewards {
		b.Rewards[i].InitialProbability = float64(b.Rewards[i].Quantity) / float64(total)
		b.Rewards[i].RemainingProbability = 0
		if b.RemainingCount > 0 {
			b.Rewards[i].RemainingProbability = float64(b.Rewards[i].Remaining) / float64(b.RemainingCount)
		}
	}
	return nil
}
