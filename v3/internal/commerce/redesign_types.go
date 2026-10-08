package commerce

import (
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

const ConversionTerms = "legacy-wallet-v1"
const ResetCardTerms = "reset-card-v1"

type ConversionRule struct {
	ID                         int64          `json:"id"`
	PlanID                     int64          `json:"plan_id"`
	BasisKey                   string         `json:"basis_key"`
	SourceCredits              credits.Micro  `json:"source_credits"`
	WalletCredits              credits.Micro  `json:"wallet_credits"`
	PaidWalletCredits          credits.Micro  `json:"paid_wallet_credits"`
	RecognizedRevenueCredits   *credits.Micro `json:"recognized_revenue_credits,omitempty"`
	RefreshedPaidWalletCredits *credits.Micro `json:"refreshed_paid_wallet_credits,omitempty"`
	Enabled                    bool           `json:"enabled"`
	Reviewed                   bool           `json:"reviewed"`
	Revision                   int64          `json:"revision"`
	Note                       string         `json:"note"`
}

type ResetCardRule struct {
	ID                     int64         `json:"id"`
	Name                   string        `json:"name"`
	ReferencePlanID        int64         `json:"reference_plan_id"`
	CardPlanID             int64         `json:"card_plan_id"`
	Credits                credits.Micro `json:"credits"`
	CostPerCard            credits.Micro `json:"cost_per_card"`
	BaselineCost           credits.Micro `json:"baseline_cost"`
	BudgetTotal            credits.Micro `json:"budget_total"`
	BudgetReserved         credits.Micro `json:"budget_reserved"`
	IncrementalBudgetTotal credits.Micro `json:"incremental_budget_total"`
	IncrementalReserved    credits.Micro `json:"incremental_reserved"`
	Enabled                bool          `json:"enabled"`
	Reviewed               bool          `json:"reviewed"`
	Revision               int64         `json:"revision"`
	Note                   string        `json:"note"`
}
type RedesignRules struct {
	ConversionRules []ConversionRule `json:"conversion_rules"`
	CardRules       []ResetCardRule  `json:"card_rules"`
}
type WalletConversionQuote struct {
	QuoteID               string                    `json:"quote_id"`
	SubscriptionID        int64                     `json:"subscription_id"`
	State                 string                    `json:"state"`
	ReviewReason          string                    `json:"review_reason,omitempty"`
	BasisKey              string                    `json:"basis_key"`
	RuleID                int64                     `json:"rule_id"`
	RuleRevision          int64                     `json:"rule_revision"`
	SourceTotal           credits.Micro             `json:"source_total"`
	SourceCredits         credits.Micro             `json:"source_credits"`
	TargetCredits         credits.Micro             `json:"target_credits"`
	PaidCredits           credits.Micro             `json:"paid_credits"`
	RewardCredits         credits.Micro             `json:"reward_credits"`
	SubscriptionExpiresAt time.Time                 `json:"subscription_expires_at"`
	ExpiresAt             time.Time                 `json:"expires_at"`
	TermsVersion          string                    `json:"terms_version"`
	ReviewID              int64                     `json:"review_id,omitempty"`
	FutureCredits         credits.Micro             `json:"future_credits,omitempty"`
	Segments              []WalletConversionSegment `json:"segments,omitempty"`
}
type WalletConversion struct {
	RequestID       string        `json:"request_id"`
	QuoteID         string        `json:"quote_id"`
	SubscriptionID  int64         `json:"subscription_id"`
	State           string        `json:"state"`
	SourceCredits   credits.Micro `json:"source_credits"`
	TargetCredits   credits.Micro `json:"target_credits"`
	PaidCredits     credits.Micro `json:"paid_credits"`
	WalletAccountID *int64        `json:"wallet_account_id,omitempty"`
	FailureReason   string        `json:"failure_reason,omitempty"`
	CompletedAt     *time.Time    `json:"completed_at,omitempty"`
}
type ResetCardQuote struct {
	QuoteID        string    `json:"quote_id"`
	RuleID         int64     `json:"rule_id"`
	RuleRevision   int64     `json:"rule_revision"`
	Quantity       int       `json:"quantity"`
	AvailableCount int64     `json:"available_count"`
	Plan           Plan      `json:"plan"`
	ExpiresAt      time.Time `json:"expires_at"`
	TermsVersion   string    `json:"terms_version"`
}
type BoundSubscriptionCard struct {
	ID             int64      `json:"id"`
	State          string     `json:"state"`
	Plan           Plan       `json:"plan"`
	SubscriptionID *int64     `json:"subscription_id,omitempty"`
	ActivatedAt    *time.Time `json:"activated_at,omitempty"`
}
type ResetCardExchange struct {
	RequestID      string                  `json:"request_id"`
	QuoteID        string                  `json:"quote_id"`
	Quantity       int                     `json:"quantity"`
	RemainingCount int64                   `json:"remaining_count"`
	Cards          []BoundSubscriptionCard `json:"cards"`
}
type RedesignCostPreview struct {
	ActiveLegacyCount   int64                   `json:"active_legacy_count"`
	AvailableResetCount int64                   `json:"available_reset_count"`
	Rules               RedesignRules           `json:"rules"`
	Candidates          []WalletConversionQuote `json:"candidates"`
}
