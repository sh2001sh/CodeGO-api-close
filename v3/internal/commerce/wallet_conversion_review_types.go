package commerce

import (
	"time"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// A review records source-specific valuation, not a mutable override of the
// subscription. Current and promised future credits remain separately visible.
type WalletConversionSegment struct {
	Name                 string        `json:"name"`
	OriginalOrderID      int64         `json:"original_order_id"`
	SourceTotal          credits.Micro `json:"source_total"`
	CurrentCredits       credits.Micro `json:"current_credits"`
	FutureCredits        credits.Micro `json:"future_credits"`
	WalletCredits        credits.Micro `json:"wallet_credits"`
	PaidWalletCredits    credits.Micro `json:"paid_wallet_credits"`
	TargetCredits        credits.Micro `json:"target_credits"`
	PaidCredits          credits.Micro `json:"paid_credits"`
	RevenueMultiplierPPM int64         `json:"revenue_multiplier_ppm"`
}

type WalletConversionReview struct {
	ID             int64                     `json:"id"`
	SubscriptionID int64                     `json:"subscription_id"`
	FactHash       string                    `json:"fact_hash"`
	Revision       int64                     `json:"revision"`
	Segments       []WalletConversionSegment `json:"segments"`
	Note           string                    `json:"note"`
	Enabled        bool                      `json:"enabled"`
	Reviewed       bool                      `json:"reviewed"`
	ReviewerID     int64                     `json:"reviewer_id"`
	ReviewedAt     time.Time                 `json:"reviewed_at"`
}

type WalletConversionSource struct {
	OrderID        int64          `json:"order_id"`
	State          string         `json:"state"`
	PurchaseType   string         `json:"purchase_type"`
	Credits        credits.Micro  `json:"credits"`
	RevenueCredits *credits.Micro `json:"revenue_credits,omitempty"`
	PreviouslyPaid credits.Micro  `json:"previously_paid"`
	PendingRefund  bool           `json:"pending_refund"`
}

type WalletConversionReviewEvidence struct {
	SubscriptionID int64                    `json:"subscription_id"`
	UserID         int64                    `json:"user_id"`
	FactHash       string                   `json:"fact_hash"`
	CurrentCredits credits.Micro            `json:"current_credits"`
	FutureCredits  credits.Micro            `json:"future_credits"`
	ExpiresAt      time.Time                `json:"expires_at"`
	ResetUsed      bool                     `json:"reset_used"`
	Sources        []WalletConversionSource `json:"sources"`
	Review         *WalletConversionReview  `json:"review,omitempty"`
}
