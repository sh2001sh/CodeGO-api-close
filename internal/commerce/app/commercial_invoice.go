package app

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrCommercialInvoiceInvalid         = errors.New("invoice.invalidDetails")
	ErrCommercialInvoiceDetailsRequired = errors.New("invoice.detailsRequired")
	ErrCommercialInvoiceConflict        = errors.New("invoice.detailsConflict")
)

const commercialSellerAddress = "UNIT 1618A, 16/F, PIONEER CENTRE, 750 NATHAN ROAD, MONG KOK, HONG KONG"

const invoiceArchivePrefix = "codego-invoice-gzip:"

func isCommercialInvoiceArchive(value string) bool {
	return strings.HasPrefix(value, invoiceArchivePrefix) || strings.HasPrefix(value, `{"commercial_invoice_version":`)
}

func decodeInvoiceArchive(value string, archive *commercialInvoiceArchive) error {
	if strings.HasPrefix(value, invoiceArchivePrefix) {
		compressed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, invoiceArchivePrefix))
		if err != nil {
			return err
		}
		reader, err := gzip.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return err
		}
		defer reader.Close()
		raw, err := io.ReadAll(io.LimitReader(reader, 32<<20))
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, archive)
	}
	return json.Unmarshal([]byte(value), archive)
}

func encodeInvoiceArchive(archive commercialInvoiceArchive) (string, error) {
	raw, err := json.Marshal(archive)
	if err != nil {
		return "", err
	}
	if len(raw) >= 32<<20 {
		return "", ErrCommercialInvoiceConflict
	}
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err = writer.Write(raw); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}
	return invoiceArchivePrefix + base64.StdEncoding.EncodeToString(buffer.Bytes()), nil
}

// Payment currencies come from the immutable provider path or its saved payload,
// never from today's exchange-rate/display settings or a mutable plan.
func commercialPaymentCurrency(provider, method, payload string, amount float64) string {
	if provider == "" {
		provider = method
	}
	switch provider {
	case commerceschema.PaymentProviderStripe:
		var paid stripePaymentEvidence
		if json.Unmarshal([]byte(payload), &paid) == nil && paid.Checkout != nil {
			if _, _, err := validateStripePayment(payload, payload, paid.Reference, amount); err != nil {
				return ""
			}
			return strings.ToUpper(paid.Currency)
		}
		// Historical Stripe payments predate per-order currency snapshots.
		return "USD"
	case PaymentProviderNowPayments:
		return "USDT"
	case commerceschema.PaymentProviderEpay, commerceschema.PaymentProviderXunhu, "wxpay", "alipay":
		return "CNY"
	case commerceschema.PaymentProviderCreem:
		var event CreemWebhookEvent
		// The V3 document has no tax line items. Require a matching, tax-free
		// payment snapshot rather than presenting the configured price as paid.
		if json.Unmarshal([]byte(payload), &event) == nil && event.Object.Order.TaxAmount == 0 && event.Object.Order.AmountPaid > 0 && int64(event.Object.Order.AmountPaid) == int64(math.Round(amount*100)) {
			return strings.ToUpper(strings.TrimSpace(event.Object.Order.Currency))
		}
	}
	return ""
}

type InvoiceBuyer struct {
	BuyerName    string `json:"buyer_name"`
	BuyerAddress string `json:"buyer_address"`
	BuyerCountry string `json:"buyer_country,omitempty"`
	BuyerTaxID   string `json:"buyer_tax_id,omitempty"`
}

type CommercialInvoiceInput struct {
	InvoiceBuyer
	PreviousNumber string `json:"previous_number,omitempty"`
	Reason         string `json:"reason,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
}

// The existing invoice request's text field holds frozen documents; no schema or
// financial-ledger changes are needed. Old manual requests remain untouched.
type commercialInvoiceArchive struct {
	Version   int                         `json:"commercial_invoice_version"`
	Documents []CommercialInvoiceDocument `json:"documents"`
}

type CommercialInvoiceDocument struct {
	Number        string       `json:"number"`
	DocumentType  string       `json:"document_type"`
	IssuedAt      int64        `json:"issued_at"`
	RelatedNumber string       `json:"related_number,omitempty"`
	Reason        string       `json:"reason,omitempty"`
	Buyer         InvoiceBuyer `json:"buyer"`
	RequestID     string       `json:"request_id,omitempty"`
	RefundAmount  float64      `json:"refund_amount,omitempty"`
	PDF           []byte       `json:"pdf,omitempty"`
}

type orderInvoiceData struct {
	number, buyer, buyerAddress, sellerAddress, description, trade, provider, currency, amount string
	sellerBRN, paymentReference, website, buyerCountry, buyerTaxID                             string
	documentType, relatedNumber, correctionReason                                              string
	details                                                                                    []string
	paid, issued                                                                               time.Time
}

type commercialOrder struct {
	amount                                             float64
	currency, title, provider, reference, refundStatus string
	paidAt, refundAt                                   int64
	refundAmount                                       float64
}

func loadCommercialOrder(tx *gorm.DB, userID int, source, trade string) (commercialOrder, error) {
	var result commercialOrder
	lock := tx.Clauses(clause.Locking{Strength: "UPDATE"})
	switch source {
	case commerceschema.InvoiceSourceTopUp:
		var order commerceschema.TopUp
		if err := lock.Where("user_id = ? AND trade_no = ? AND status = ? AND money > 0 AND complete_time > 0", userID, trade, constant.TopUpStatusSuccess).First(&order).Error; err != nil {
			return result, ErrInvoiceOrderUnavailable
		}
		var mirrored int64
		if err := tx.Model(&commerceschema.SubscriptionOrder{}).Where("trade_no = ?", trade).Count(&mirrored).Error; err != nil {
			return result, err
		}
		if mirrored > 0 {
			return result, ErrInvoiceOrderUnavailable
		}
		result = commercialOrder{amount: order.Money, currency: commercialPaymentCurrency(order.PaymentProvider, order.PaymentMethod, order.ProviderPayload, order.Money), title: "API account credit", provider: order.PaymentProvider, reference: order.ExternalPaymentID, paidAt: order.CompleteTime, refundStatus: order.RefundStatus, refundAmount: order.RefundAmount, refundAt: order.RefundUpdatedAt}
	case commerceschema.InvoiceSourceSubscription:
		var order commerceschema.SubscriptionOrder
		if err := lock.Where("user_id = ? AND trade_no = ? AND status = ? AND money > 0 AND complete_time > 0", userID, trade, constant.TopUpStatusSuccess).First(&order).Error; err != nil {
			return result, ErrInvoiceOrderUnavailable
		}
		result = commercialOrder{amount: order.Money, currency: commercialPaymentCurrency(order.PaymentProvider, order.PaymentMethod, order.ProviderPayload, order.Money), title: "AI subscription", provider: order.PaymentProvider, paidAt: order.CompleteTime, refundStatus: order.RefundStatus, refundAmount: order.RefundAmount, refundAt: order.RefundUpdatedAt}
		var plan commerceschema.SubscriptionPlan
		if err := tx.First(&plan, order.PlanId).Error; err == nil {
			result.title += " / " + plan.Title
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return result, err
		}
	default:
		return result, ErrInvoiceOrderUnavailable
	}
	if !math.IsNaN(result.amount) && !math.IsInf(result.amount, 0) && result.amount > 0 {
		return result, nil
	}
	return result, ErrInvoiceOrderUnavailable
}

func invoiceTextValid(value string, limit int, multiline bool) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) && (!multiline || r != '\n') }) < 0
}

func normalizeCommercialInput(input *CommercialInvoiceInput) error {
	input.BuyerName = strings.TrimSpace(input.BuyerName)
	input.BuyerAddress = strings.ReplaceAll(strings.TrimSpace(input.BuyerAddress), "\r\n", "\n")
	input.BuyerCountry = strings.TrimSpace(input.BuyerCountry)
	input.BuyerTaxID = strings.TrimSpace(input.BuyerTaxID)
	input.Reason = strings.TrimSpace(input.Reason)
	if !invoiceTextValid(input.BuyerName, 200, false) || !invoiceTextValid(input.BuyerAddress, 600, true) || (input.BuyerCountry != "" && !invoiceTextValid(input.BuyerCountry, 100, false)) || (input.BuyerTaxID != "" && !invoiceTextValid(input.BuyerTaxID, 64, false)) {
		return ErrCommercialInvoiceInvalid
	}
	return nil
}

// action is download, issue, correct, credit, or list. The order lock serializes
// issuance with refunds and repeated clicks; every lookup is scoped to its owner.
func CommercialInvoice(userID int, source, trade, action, number string, input *CommercialInvoiceInput) ([]CommercialInvoiceDocument, error) {
	if userID <= 0 || !invoiceTextValid(trade, 255, false) {
		return nil, ErrInvoiceOrderUnavailable
	}
	if action == "issue" || action == "correct" {
		if input == nil {
			return nil, ErrCommercialInvoiceInvalid
		}
		if err := normalizeCommercialInput(input); err != nil {
			return nil, err
		}
	}
	if action == "correct" && (!invoiceTextValid(input.Reason, 300, false) || !invoiceTextValid(input.RequestID, 128, false)) {
		return nil, ErrCommercialInvoiceInvalid
	}
	var result []CommercialInvoiceDocument
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		order, err := loadCommercialOrder(tx, userID, source, trade)
		if err != nil {
			return err
		}
		var request commerceschema.InvoiceRequest
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND source_type = ? AND trade_no = ?", userID, source, trade).First(&request).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		archive := commercialInvoiceArchive{Version: 1}
		if exists && (decodeInvoiceArchive(request.Remark, &archive) != nil || archive.Version != 1 || len(archive.Documents) == 0) {
			return ErrCommercialInvoiceConflict
		}
		if exists {
			order.amount, order.currency, order.title = request.OrderAmount, request.Currency, request.OrderTitle
		}
		if action == "list" {
			result = archive.Documents
			return nil
		}
		latest := -1
		for index, document := range archive.Documents {
			if document.DocumentType == "invoice" {
				latest = index
			}
		}
		if action == "download" {
			for index := len(archive.Documents) - 1; index >= 0; index-- {
				d := archive.Documents[index]
				if (number == "" && d.DocumentType == "invoice") || d.Number == number {
					result = []CommercialInvoiceDocument{d}
					return nil
				}
			}
			if number != "" {
				return ErrInvoiceOrderUnavailable
			}
			return ErrCommercialInvoiceDetailsRequired
		}
		if action == "issue" && latest >= 0 {
			if archive.Documents[latest].Buyer != input.InvoiceBuyer {
				return ErrCommercialInvoiceConflict
			}
			result = []CommercialInvoiceDocument{archive.Documents[latest]}
			return nil
		}
		if action == "correct" {
			for _, d := range archive.Documents {
				if d.RequestID == input.RequestID {
					if d.Buyer != input.InvoiceBuyer || d.Reason != input.Reason || d.RelatedNumber != input.PreviousNumber {
						return ErrCommercialInvoiceConflict
					}
					result = []CommercialInvoiceDocument{d}
					return nil
				}
			}
			if latest < 0 || archive.Documents[latest].Number != input.PreviousNumber {
				return ErrCommercialInvoiceConflict
			}
		}
		if action == "credit" {
			if latest < 0 || order.refundStatus != commerceschema.RefundStatusSuccess || order.refundAmount <= 0 || order.refundAmount > order.amount {
				return ErrInvoiceOrderUnavailable
			}
			for _, d := range archive.Documents {
				if d.DocumentType == "credit_note" && d.RefundAmount == order.refundAmount {
					result = []CommercialInvoiceDocument{d}
					return nil
				}
			}
		}
		if action != "issue" && action != "correct" && action != "credit" {
			return ErrCommercialInvoiceInvalid
		}
		if resultCurrency := strings.TrimSpace(order.currency); resultCurrency == "" || len(resultCurrency) > 8 {
			return ErrInvoiceOrderUnavailable
		}
		if action != "credit" && order.refundStatus != "" && order.refundStatus != commerceschema.RefundStatusFailed {
			return ErrInvoiceOrderUnavailable
		}
		if !exists {
			var reserved int64
			if err := tx.Model(&commerceschema.InvoiceRequestItem{}).Where("source_type = ? AND trade_no = ?", source, trade).Count(&reserved).Error; err != nil {
				return err
			}
			if reserved > 0 {
				return ErrCommercialInvoiceConflict
			}
			request = commerceschema.InvoiceRequest{UserID: userID, SourceType: source, TradeNo: trade, OrderAmount: order.amount, Currency: order.currency, OrderTitle: order.title, OrderCount: 1, InvoiceType: commerceschema.InvoiceTypePersonal, Title: input.BuyerName, TaxNumber: input.BuyerTaxID, Status: commerceschema.InvoiceStatusIssued, DeliveryMethod: "download"}
			if err := tx.Create(&request).Error; err != nil {
				return err
			}
			if err := tx.Create(&commerceschema.InvoiceRequestItem{InvoiceID: request.ID, UserID: userID, SourceType: source, TradeNo: trade, OrderAmount: order.amount, Currency: order.currency, OrderTitle: order.title, PaidAt: order.paidAt}).Error; err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		document := CommercialInvoiceDocument{Number: fmt.Sprintf("CG-%04d-%012d", now.In(time.FixedZone("HKT", 28800)).Year(), request.ID), DocumentType: "invoice", IssuedAt: now.Unix()}
		if action == "credit" {
			document.Buyer = archive.Documents[latest].Buyer
			document.DocumentType = "credit_note"
			document.RefundAmount = order.refundAmount
			document.RelatedNumber = archive.Documents[latest].Number
			document.Reason = "Confirmed payment refund"
		} else {
			document.Buyer = input.InvoiceBuyer
		}
		if exists {
			document.Number = request.InvoiceNumber + fmt.Sprintf("-%s%d", map[bool]string{true: "CN", false: "R"}[action == "credit"], len(archive.Documents)+1)
		}
		if action == "correct" {
			document.RelatedNumber = input.PreviousNumber
			document.Reason = input.Reason
			document.RequestID = input.RequestID
		}
		amount := order.amount
		paid := time.Unix(order.paidAt, 0)
		if action == "credit" {
			previousRefund := 0.0
			for _, d := range archive.Documents {
				if d.RefundAmount > previousRefund {
					previousRefund = d.RefundAmount
				}
			}
			amount = order.refundAmount - previousRefund
			if amount <= 0 {
				return ErrInvoiceOrderUnavailable
			}
			paid = time.Unix(order.refundAt, 0)
		}
		amountDigits := 2
		if order.currency == "USDT" {
			amountDigits = 6
		}
		document.PDF, err = renderOrderInvoice(orderInvoiceData{number: document.Number, buyer: document.Buyer.BuyerName, buyerAddress: document.Buyer.BuyerAddress, buyerCountry: document.Buyer.BuyerCountry, buyerTaxID: document.Buyer.BuyerTaxID, sellerAddress: commercialSellerAddress, sellerBRN: "81318858", description: order.title, trade: trade, provider: order.provider, paymentReference: order.reference, currency: strings.ToUpper(order.currency), amount: fmt.Sprintf("%.*f", amountDigits, amount), paid: paid, issued: now, documentType: document.DocumentType, relatedNumber: document.RelatedNumber, correctionReason: document.Reason})
		if err != nil {
			return err
		}
		archive.Documents = append(archive.Documents, document)
		raw, err := encodeInvoiceArchive(archive)
		if err != nil {
			return err
		}
		if tx.Dialector.Name() == "mysql" && len(raw) > 65535 {
			return ErrCommercialInvoiceConflict
		}
		updates := map[string]any{"remark": raw}
		if !exists {
			updates["invoice_number"] = document.Number
			updates["issued_at"] = document.IssuedAt
		}
		if err = tx.Model(&request).Updates(updates).Error; err != nil {
			return err
		}
		result = []CommercialInvoiceDocument{document}
		return nil
	})
	return result, err
}
