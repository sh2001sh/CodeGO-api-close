package commerce

import (
	"encoding/json"
	"time"
)

type InvoiceOrderInput struct {
	SourceType string `json:"source_type"`
	TradeNo    string `json:"trade_no"`
}

type CreateInvoiceRequestInput struct {
	Orders      []InvoiceOrderInput `json:"orders"`
	SourceType  string              `json:"source_type"`
	TradeNo     string              `json:"trade_no"`
	InvoiceType string              `json:"invoice_type"`
	Title       string              `json:"title"`
	TaxNumber   string              `json:"tax_number"`
	Email       string              `json:"email"`
	Remark      string              `json:"remark"`
}

type UpdateInvoiceRequestInput struct {
	Status        string `json:"status"`
	InvoiceNumber string `json:"invoice_number"`
	AdminNote     string `json:"admin_note"`
}

type InvoiceEligibleOrder struct {
	SourceType       string      `json:"source_type"`
	TradeNo          string      `json:"trade_no"`
	OrderTitle       string      `json:"order_title"`
	OrderAmountMinor int64       `json:"order_amount_minor"`
	OrderAmount      json.Number `json:"order_amount"`
	Currency         string      `json:"currency"`
	PaidAt           int64       `json:"paid_at"`
	Requested        bool        `json:"requested"`
}

type InvoiceRequest struct {
	ID               int64       `json:"id"`
	UserID           int64       `json:"user_id"`
	SourceType       string      `json:"source_type"`
	TradeNo          string      `json:"trade_no"`
	OrderAmountMinor int64       `json:"order_amount_minor"`
	OrderAmount      json.Number `json:"order_amount"`
	Currency         string      `json:"currency"`
	OrderTitle       string      `json:"order_title"`
	OrderCount       int         `json:"order_count"`
	InvoiceType      string      `json:"invoice_type"`
	Title            string      `json:"title"`
	TaxNumber        string      `json:"tax_number"`
	Email            string      `json:"email"`
	Remark           string      `json:"remark"`
	Status           string      `json:"status"`
	InvoiceNumber    string      `json:"invoice_number"`
	DeliveryMethod   string      `json:"delivery_method"`
	DocumentURL      string      `json:"document_url"`
	AdminNote        string      `json:"admin_note"`
	HandledBy        int64       `json:"handled_by"`
	IssuedAt         int64       `json:"issued_at"`
	CreatedAt        int64       `json:"created_at"`
	UpdatedAt        int64       `json:"updated_at"`
}

type InvoicePage struct {
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int64            `json:"total"`
	Items    []InvoiceRequest `json:"items"`
}

const invoiceColumns = `id,user_id,source_type,trade_no,order_amount_minor,currency,order_title,order_count,
invoice_type,title,tax_number,email,remark,status,invoice_number,delivery_method,document_url,admin_note,
handled_by,issued_at,created_at,updated_at`

func scanInvoice(row scanner) (InvoiceRequest, error) {
	var v InvoiceRequest
	var issued *time.Time
	var created, updated time.Time
	err := row.Scan(&v.ID, &v.UserID, &v.SourceType, &v.TradeNo, &v.OrderAmountMinor, &v.Currency, &v.OrderTitle,
		&v.OrderCount, &v.InvoiceType, &v.Title, &v.TaxNumber, &v.Email, &v.Remark, &v.Status, &v.InvoiceNumber,
		&v.DeliveryMethod, &v.DocumentURL, &v.AdminNote, &v.HandledBy, &issued, &created, &updated)
	v.OrderAmount = invoiceAmount(v.OrderAmountMinor, v.Currency)
	v.CreatedAt, v.UpdatedAt = created.Unix(), updated.Unix()
	if issued != nil {
		v.IssuedAt = issued.Unix()
	}
	return v, err
}

// JSON keeps the existing numeric amount field without floating point money.
func invoiceAmount(minor int64, currency string) json.Number {
	return json.Number(formatCurrencyMinor(minor, currency))
}
