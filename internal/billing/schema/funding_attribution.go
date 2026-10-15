package billingschema

import (
	"strings"
	"time"

	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	FundingSourceTopup              = "topup"
	FundingSourceBlindBox           = "blind_box"
	FundingSourceSubscription       = "subscription"
	FundingSourceLegacyUnattributed = "legacy_unattributed"
	FundingSourceOther              = "other"
)

// FundingSourcePolicy defines the root-managed realized-value multiplier for
// one quota origin. The multiplier is snapshotted into a funding lot on credit.
type FundingSourcePolicy struct {
	Source            string    `json:"source" gorm:"column:source;primaryKey;size:32"`
	RevenueMultiplier float64   `json:"revenue_multiplier" gorm:"column:revenue_multiplier;not null;default:0"`
	UpdatedAt         time.Time `json:"updated_at" gorm:"column:updated_at;autoCreateTime;autoUpdateTime"`
}

func (FundingSourcePolicy) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "billing.funding_source_policies"
	}
	return "billing_funding_source_policies"
}

// FundingLot is an immutable origin batch for wallet quota. It lets a mixed
// wallet be attributed at settlement time without relying on current balance.
type FundingLot struct {
	LotID             string    `json:"lot_id" gorm:"column:lot_id;primaryKey;size:64"`
	AccountID         string    `json:"account_id" gorm:"column:account_id;size:64;index:idx_billing_funding_lots_account_created"`
	Source            string    `json:"source" gorm:"column:source;size:32;index"`
	ReferenceType     string    `json:"reference_type" gorm:"column:reference_type;size:32"`
	ReferenceID       string    `json:"reference_id" gorm:"column:reference_id;size:128"`
	IdempotencyKey    string    `json:"-" gorm:"column:idempotency_key;size:255;uniqueIndex:uq_billing_funding_lots_idempotency"`
	OriginalAmount    int64     `json:"original_amount" gorm:"column:original_amount"`
	RemainingAmount   int64     `json:"remaining_amount" gorm:"column:remaining_amount;index"`
	RevenueMultiplier float64   `json:"revenue_multiplier" gorm:"column:revenue_multiplier"`
	CreatedAt         time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (FundingLot) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "billing.funding_lots"
	}
	return "billing_funding_lots"
}

func (lot *FundingLot) BeforeCreate(_ *gorm.DB) error {
	if strings.TrimSpace(lot.LotID) == "" {
		lot.LotID = platformruntime.GetUUID()
	}
	return nil
}

// FundingAllocation records the exact quota origin used by one settled request.
type FundingAllocation struct {
	AllocationID      string    `json:"allocation_id" gorm:"column:allocation_id;primaryKey;size:64"`
	RequestID         string    `json:"request_id" gorm:"column:request_id;size:64;index:idx_billing_funding_allocations_request_lot,unique"`
	LotID             string    `json:"lot_id" gorm:"column:lot_id;size:64;index:idx_billing_funding_allocations_request_lot,unique"`
	AccountID         string    `json:"account_id" gorm:"column:account_id;size:64;index"`
	Source            string    `json:"source" gorm:"column:source;size:32;index"`
	Amount            int64     `json:"amount" gorm:"column:amount"`
	RevenueMultiplier float64   `json:"revenue_multiplier" gorm:"column:revenue_multiplier"`
	CreatedAt         time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (FundingAllocation) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "billing.funding_allocations"
	}
	return "billing_funding_allocations"
}

func (allocation *FundingAllocation) BeforeCreate(_ *gorm.DB) error {
	if strings.TrimSpace(allocation.AllocationID) == "" {
		allocation.AllocationID = platformruntime.GetUUID()
	}
	return nil
}

// RequestEconomics is an internal settlement snapshot. It is intentionally
// stored outside user-visible logs so procurement information remains root-only.
type RequestEconomics struct {
	RequestID                 string    `json:"request_id" gorm:"column:request_id;primaryKey;size:64"`
	ChannelID                 int       `json:"channel_id" gorm:"column:channel_id;index"`
	RoutePoolID               int64     `json:"route_pool_id" gorm:"column:route_pool_id;index"`
	ActualAmount              int64     `json:"actual_amount" gorm:"column:actual_amount"`
	BillingSource             string    `json:"billing_source" gorm:"column:billing_source;size:32;index"`
	SubscriptionID            int       `json:"subscription_id" gorm:"column:subscription_id;index"`
	ProcurementCostMultiplier float64   `json:"procurement_cost_multiplier" gorm:"column:procurement_cost_multiplier"`
	RevenueMultiplier         float64   `json:"revenue_multiplier" gorm:"column:revenue_multiplier"`
	SettledAt                 time.Time `json:"settled_at" gorm:"column:settled_at;index"`
	CreatedAt                 time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

func (RequestEconomics) TableName() string {
	if platformdb.UsingPostgreSQL {
		return "billing.request_economics"
	}
	return "billing_request_economics"
}
