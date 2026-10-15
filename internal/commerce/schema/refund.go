package schema

const (
	RefundOrderTypeBalance      = "balance"
	RefundOrderTypeSubscription = "subscription"
	RefundStatusProcessing      = "processing"
	RefundStatusSuccess         = "success"
	RefundStatusFailed          = "failed"
)

type RefundRequest struct {
	OrderType string `json:"order_type" binding:"required"`
	TradeNo   string `json:"trade_no" binding:"required"`
}

type RefundableOrder struct {
	OrderType         string  `json:"order_type"`
	TradeNo           string  `json:"trade_no"`
	PaymentMethod     string  `json:"payment_method"`
	CreatedAt         int64   `json:"created_at"`
	PaidAmount        float64 `json:"paid_amount"`
	TotalQuota        int64   `json:"total_quota"`
	UsedQuota         int64   `json:"used_quota"`
	RemainingQuota    int64   `json:"remaining_quota"`
	GrossRefund       float64 `json:"gross_refund"`
	FeeAmount         float64 `json:"fee_amount"`
	RefundAmount      float64 `json:"refund_amount"`
	RefundStatus      string  `json:"refund_status"`
	Refundable        bool    `json:"refundable"`
	UnavailableReason string  `json:"unavailable_reason,omitempty"`
}

type RefundResult struct {
	OrderType    string  `json:"order_type"`
	TradeNo      string  `json:"trade_no"`
	RefundNo     string  `json:"refund_no"`
	RefundID     string  `json:"refund_id,omitempty"`
	RefundAmount float64 `json:"refund_amount"`
	Status       string  `json:"status"`
}
