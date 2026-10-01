package commerce

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

var ErrRefundUnavailable = errors.New("commerce: this order has no refundable unused credits")

type RefundPayment struct {
	OrderID, RefundNo string
	AmountMinor       int64
}
type RefundProviderResult struct {
	RefundNo, RefundID string
	AmountMinor        int64
	// State is processing, success or failed. Only an authenticated provider
	// response can release the reservation or finalize the refund.
	State string
}
type RefundProvider interface {
	// CreateRefund must use RefundNo as the merchant idempotency key. A lost
	// response or crash before dispatch can submit the same fixed request again.
	CreateRefund(context.Context, RefundPayment) (RefundProviderResult, error)
	QueryRefund(context.Context, string, string) (RefundProviderResult, error)
}

type UserRefunds struct {
	pool     *pgxpool.Pool
	poster   TransactionPoster
	rdb      *redisx.Client
	provider RefundProvider
	now      func() time.Time
}

// Redis must be supplied for live gateway accounts. Nil is only for isolated
// offline verification, where no request can settle asynchronously.
func NewUserRefunds(pool *pgxpool.Pool, poster TransactionPoster, rdb *redisx.Client, provider RefundProvider) *UserRefunds {
	return &UserRefunds{pool: pool, poster: poster, rdb: rdb, provider: provider, now: time.Now}
}

type UserRefundRequest struct {
	OrderType string `json:"order_type"`
	TradeNo   string `json:"trade_no"`
}
type RefundableOrder struct {
	OrderType         string        `json:"order_type"`
	TradeNo           string        `json:"trade_no"`
	PaymentMethod     string        `json:"payment_method"`
	CreatedAt         int64         `json:"created_at"`
	PaidAmount        json.Number   `json:"paid_amount"`
	TotalQuota        credits.Micro `json:"total_quota"`
	UsedQuota         credits.Micro `json:"used_quota"`
	RemainingQuota    credits.Micro `json:"remaining_quota"`
	GrossRefund       json.Number   `json:"gross_refund"`
	FeeAmount         json.Number   `json:"fee_amount"`
	RefundAmount      json.Number   `json:"refund_amount"`
	RefundAmountMinor int64         `json:"refund_amount_minor"`
	RefundStatus      string        `json:"refund_status"`
	Refundable        bool          `json:"refundable"`
	UnavailableReason string        `json:"unavailable_reason,omitempty"`
}
type UserRefundResult struct {
	OrderType    string      `json:"order_type"`
	TradeNo      string      `json:"trade_no"`
	RefundNo     string      `json:"refund_no"`
	RefundID     string      `json:"refund_id,omitempty"`
	RefundAmount json.Number `json:"refund_amount"`
	AmountMinor  int64       `json:"refund_amount_minor"`
	Status       string      `json:"status"`
	Message      string      `json:"message,omitempty"`
}
type userRefundRecord struct {
	UserRefundResult
	orderID, userID, accountID, subscriptionID int64
	providerOrderID, originalState             string
	credits, reserved                          credits.Micro
}

func proportionalMinor(paid int64, remaining, total credits.Micro) int64 {
	if paid <= 0 || remaining <= 0 || total <= 0 {
		return 0
	}
	remaining = min(remaining, total)
	n := new(big.Int).Mul(big.NewInt(paid), big.NewInt(int64(remaining)))
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, big.NewInt(int64(total)), r)
	if r.Lsh(r, 1).Cmp(big.NewInt(int64(total))) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	return q.Int64()
}

func refundQuote(o Order, remaining, total credits.Micro, status string) RefundableOrder {
	gross := proportionalMinor(o.AmountMinor, remaining, total)
	fee := proportionalMinor(gross, 2, 100)
	typ := "balance"
	if o.Kind == "subscription" {
		typ = "subscription"
	}
	item := RefundableOrder{OrderType: typ, TradeNo: o.TradeNo, PaymentMethod: o.Provider, CreatedAt: o.CreatedAt.Unix(),
		PaidAmount: json.Number(formatMinor(o.AmountMinor)), TotalQuota: total, UsedQuota: max(total-remaining, 0), RemainingQuota: remaining,
		GrossRefund: json.Number(formatMinor(gross)), FeeAmount: json.Number(formatMinor(fee)), RefundAmount: json.Number(formatMinor(gross - fee)),
		RefundAmountMinor: gross - fee, RefundStatus: status}
	item.Refundable = o.Provider == "epay" && o.Currency == "cny" && o.State == "paid" && remaining > 0 && gross-fee > 0 && (status == "" || status == "failed")
	if !item.Refundable {
		item.UnavailableReason = "订单已使用完、退款处理中或不支持退款"
	}
	return item
}

const userRefundColumns = `r.refund_no,r.order_id,r.user_id,r.account_id,coalesce(r.subscription_id,0),r.original_subscription_state,
r.provider_order_id,r.provider_refund_id,r.amount_minor,r.refund_credits,r.reserved_credits,r.status,o.trade_no,o.kind`

func scanUserRefund(row scanner) (userRefundRecord, error) {
	var r userRefundRecord
	var kind string
	err := row.Scan(&r.RefundNo, &r.orderID, &r.userID, &r.accountID, &r.subscriptionID, &r.originalState, &r.providerOrderID,
		&r.RefundID, &r.AmountMinor, &r.credits, &r.reserved, &r.Status, &r.TradeNo, &kind)
	r.OrderType = "balance"
	if kind == "subscription" {
		r.OrderType = "subscription"
	}
	r.RefundAmount = json.Number(formatMinor(r.AmountMinor))
	return r, err
}

func refundOperation(no string) string { return "user-refund:" + no }
