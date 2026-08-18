package schema

import (
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const (
	InvoiceSourceTopUp        = "topup"
	InvoiceSourceSubscription = "subscription"
	InvoiceSourceBatch        = "batch"

	InvoiceTypePersonal   = "personal"
	InvoiceTypeEnterprise = "enterprise"

	InvoiceStatusPending  = "pending"
	InvoiceStatusIssued   = "issued"
	InvoiceStatusRejected = "rejected"
)

// InvoiceRequest is a user-submitted request for one or more paid commerce orders.
type InvoiceRequest struct {
	ID int64 `json:"id"`

	UserID     int    `json:"user_id" gorm:"not null;index"`
	SourceType string `json:"source_type" gorm:"type:varchar(32);not null;uniqueIndex:uq_invoice_request_order"`
	TradeNo    string `json:"trade_no" gorm:"type:varchar(255);not null;uniqueIndex:uq_invoice_request_order"`

	OrderAmount float64 `json:"order_amount" gorm:"type:decimal(12,2);not null"`
	Currency    string  `json:"currency" gorm:"type:varchar(8);not null;default:'CNY'"`
	OrderTitle  string  `json:"order_title" gorm:"type:varchar(255);not null"`
	OrderCount  int     `json:"order_count" gorm:"not null;default:1"`

	InvoiceType string `json:"invoice_type" gorm:"type:varchar(16);not null"`
	Title       string `json:"title" gorm:"type:varchar(255);not null"`
	TaxNumber   string `json:"tax_number" gorm:"type:varchar(64);default:''"`
	Email       string `json:"email" gorm:"type:varchar(255);not null"`
	Remark      string `json:"remark" gorm:"type:text"`

	Status         string `json:"status" gorm:"type:varchar(16);not null;default:'pending';index"`
	InvoiceNumber  string `json:"invoice_number" gorm:"type:varchar(128);default:''"`
	DeliveryMethod string `json:"delivery_method" gorm:"type:varchar(16);default:''"`
	DocumentURL    string `json:"document_url" gorm:"type:text"`
	AdminNote      string `json:"admin_note" gorm:"type:text"`
	HandledBy      int    `json:"handled_by" gorm:"not null;default:0"`

	IssuedAt  int64 `json:"issued_at" gorm:"bigint;not null;default:0"`
	CreatedAt int64 `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint;not null"`
}

// InvoiceRequestItem locks one paid order to a single invoice request.
type InvoiceRequestItem struct {
	ID          int64   `json:"id"`
	InvoiceID   int64   `json:"invoice_id" gorm:"not null;index"`
	UserID      int     `json:"user_id" gorm:"not null;index"`
	SourceType  string  `json:"source_type" gorm:"type:varchar(32);not null;uniqueIndex:uq_invoice_item_order"`
	TradeNo     string  `json:"trade_no" gorm:"type:varchar(255);not null;uniqueIndex:uq_invoice_item_order"`
	OrderAmount float64 `json:"order_amount" gorm:"type:decimal(12,2);not null"`
	Currency    string  `json:"currency" gorm:"type:varchar(8);not null;default:'CNY'"`
	OrderTitle  string  `json:"order_title" gorm:"type:varchar(255);not null"`
	PaidAt      int64   `json:"paid_at" gorm:"not null"`
}

func (InvoiceRequestItem) TableName() string { return "invoice_request_items" }

func (r *InvoiceRequest) BeforeCreate(_ *gorm.DB) error {
	now := platformruntime.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *InvoiceRequest) BeforeUpdate(_ *gorm.DB) error {
	r.UpdatedAt = platformruntime.GetTimestamp()
	return nil
}
