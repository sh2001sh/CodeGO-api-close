package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"gorm.io/gorm"
)

const commercialInvoiceBatchLimit = 100

var ErrCommercialInvoiceBatchInvalid = errors.New("invoice.invalidBatch")

// Frozen payment details also preserve currencies with more than two decimals;
// the legacy request/item amount columns alone cannot retain that precision.
type commercialInvoiceOrder struct {
	SourceType string  `json:"source_type"`
	TradeNo    string  `json:"trade_no"`
	Title      string  `json:"title"`
	Amount     float64 `json:"amount"`
	Currency   string  `json:"currency"`
	Provider   string  `json:"provider"`
	Reference  string  `json:"reference,omitempty"`
	PaidAt     int64   `json:"paid_at"`
}

func normalizeCommercialBatchOrders(orders []InvoiceOrderInput) ([]InvoiceOrderInput, error) {
	if len(orders) < 2 || len(orders) > commercialInvoiceBatchLimit {
		return nil, ErrCommercialInvoiceBatchInvalid
	}
	refs := append([]InvoiceOrderInput(nil), orders...)
	seen := make(map[string]bool, len(refs))
	for i := range refs {
		ref := &refs[i]
		ref.SourceType, ref.TradeNo = strings.TrimSpace(ref.SourceType), strings.TrimSpace(ref.TradeNo)
		key := invoiceOrderKey(ref.SourceType, ref.TradeNo)
		if (ref.SourceType != commerceschema.InvoiceSourceTopUp && ref.SourceType != commerceschema.InvoiceSourceSubscription) || !invoiceTextValid(ref.TradeNo, 255, false) || seen[key] {
			return nil, ErrCommercialInvoiceBatchInvalid
		}
		seen[key] = true
	}
	sort.Slice(refs, func(i, j int) bool {
		return invoiceOrderKey(refs[i].SourceType, refs[i].TradeNo) < invoiceOrderKey(refs[j].SourceType, refs[j].TradeNo)
	})
	return refs, nil
}

// The sorted order set identifies the invoice independently of selection order
// or network retries. An overlapping, different set must not issue a second bill.
func CommercialInvoiceBatch(userID int, input *CommercialInvoiceInput) ([]CommercialInvoiceDocument, error) {
	if input == nil {
		return nil, ErrCommercialInvoiceInvalid
	}
	refs, err := normalizeCommercialBatchOrders(input.Orders)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(struct {
		UserID int
		Orders []InvoiceOrderInput
	}{userID, refs})
	if err != nil {
		return nil, err
	}
	trade := fmt.Sprintf("batch-%x", sha256.Sum256(raw))
	return commercialInvoice(userID, commerceschema.InvoiceSourceBatch, trade, "issue", "", input, refs)
}

// Resolve a member order to its containing invoice before taking locks, then
// lock every payment in the same order for both batch and single-order actions.
func loadCommercialInvoiceTarget(tx *gorm.DB, userID int, source, trade string, refs []InvoiceOrderInput) (commercialOrder, string, string, []commercialInvoiceOrder, error) {
	var request commerceschema.InvoiceRequest
	query := tx.Where("user_id = ? AND source_type = ? AND trade_no = ?", userID, source, trade)
	if len(refs) == 0 {
		if source != commerceschema.InvoiceSourceTopUp && source != commerceschema.InvoiceSourceSubscription {
			return commercialOrder{}, "", "", nil, ErrInvoiceOrderUnavailable
		}
		var item commerceschema.InvoiceRequestItem
		err := tx.Where("user_id = ? AND source_type = ? AND trade_no = ?", userID, source, trade).First(&item).Error
		if err == nil {
			query = tx.Where("user_id = ? AND id = ?", userID, item.InvoiceID)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return commercialOrder{}, "", "", nil, err
		}
		refs = []InvoiceOrderInput{{SourceType: source, TradeNo: trade}}
	}
	err := query.First(&request).Error
	exists := err == nil
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return commercialOrder{}, "", "", nil, err
	}
	var frozen []commercialInvoiceOrder
	if exists {
		var archive commercialInvoiceArchive
		if decodeInvoiceArchive(request.Remark, &archive) != nil || archive.Version != 1 || len(archive.Documents) == 0 {
			return commercialOrder{}, "", "", nil, ErrCommercialInvoiceConflict
		}
		frozen = archive.Orders
		if request.SourceType == commerceschema.InvoiceSourceBatch {
			var items []commerceschema.InvoiceRequestItem
			if err := tx.Where("invoice_id = ? AND user_id = ?", request.ID, userID).Order("source_type, trade_no").Find(&items).Error; err != nil {
				return commercialOrder{}, "", "", nil, err
			}
			if len(items) != request.OrderCount || len(items) < 2 || len(items) > commercialInvoiceBatchLimit || len(frozen) != len(items) {
				return commercialOrder{}, "", "", nil, ErrCommercialInvoiceConflict
			}
			refs = make([]InvoiceOrderInput, len(items))
			for i, item := range items {
				refs[i] = InvoiceOrderInput{SourceType: item.SourceType, TradeNo: item.TradeNo}
			}
		}
		source, trade = request.SourceType, request.TradeNo
	}
	sort.Slice(refs, func(i, j int) bool {
		return invoiceOrderKey(refs[i].SourceType, refs[i].TradeNo) < invoiceOrderKey(refs[j].SourceType, refs[j].TradeNo)
	})
	snapshots := make([]commercialInvoiceOrder, 0, len(refs))
	live := make([]commercialOrder, 0, len(refs))
	for i, ref := range refs {
		order, err := loadCommercialOrder(tx, userID, ref.SourceType, ref.TradeNo)
		if err != nil {
			return commercialOrder{}, "", "", nil, err
		}
		snapshot := commercialInvoiceOrder{SourceType: ref.SourceType, TradeNo: ref.TradeNo, Title: order.title, Amount: order.amount, Currency: order.currency, Provider: order.provider, Reference: order.reference, PaidAt: order.paidAt}
		if len(frozen) > 0 {
			if len(frozen) != len(refs) || frozen[i].SourceType != ref.SourceType || frozen[i].TradeNo != ref.TradeNo {
				return commercialOrder{}, "", "", nil, ErrCommercialInvoiceConflict
			}
			snapshot = frozen[i]
		}
		snapshots = append(snapshots, snapshot)
		live = append(live, order)
	}
	if len(refs) == 1 {
		order := live[0]
		if len(frozen) > 0 {
			order.amount, order.currency, order.title = frozen[0].Amount, frozen[0].Currency, frozen[0].Title
		}
		order.trade = refs[0].TradeNo
		return order, source, trade, snapshots, nil
	}
	order, err := aggregateCommercialOrders(snapshots, live)
	return order, source, trade, snapshots, err
}

func commercialInvoiceDigits(currency string) int {
	if currency == "USDT" {
		return 6
	}
	return 2
}

func aggregateCommercialOrders(snapshots []commercialInvoiceOrder, live []commercialOrder) (commercialOrder, error) {
	result := commercialOrder{currency: snapshots[0].Currency, title: fmt.Sprintf("AI services / %d paid orders", len(snapshots))}
	scale := math.Pow10(commercialInvoiceDigits(result.currency))
	var total, refunded int64
	providers := make(map[string]bool)
	trades := make([]string, 0, len(snapshots))
	for i, snapshot := range snapshots {
		if result.currency == "" || snapshot.Currency != result.currency || math.IsNaN(snapshot.Amount) || math.IsInf(snapshot.Amount, 0) || snapshot.Amount <= 0 || snapshot.Amount > 9999999999.99 {
			return commercialOrder{}, ErrCommercialInvoiceBatchInvalid
		}
		minor := math.Round(snapshot.Amount * scale)
		if math.Abs(snapshot.Amount-minor/scale) > 1e-8 {
			return commercialOrder{}, ErrCommercialInvoiceBatchInvalid
		}
		total += int64(minor)
		if float64(total)/scale > 9999999999.99 {
			return commercialOrder{}, ErrCommercialInvoiceBatchInvalid
		}
		providers[snapshot.Provider] = true
		trades = append(trades, snapshot.TradeNo)
		if snapshot.PaidAt > result.paidAt {
			result.paidAt = snapshot.PaidAt
		}
		result.details = append(result.details, fmt.Sprintf("%s / %s %.*f", snapshot.Title, result.currency, commercialInvoiceDigits(result.currency), snapshot.Amount), "Order: "+snapshot.TradeNo+" / Paid "+time.Unix(snapshot.PaidAt, 0).In(time.FixedZone("HKT", 28800)).Format("2006-01-02 15:04 HKT"))
		if snapshot.Reference != "" {
			result.details = append(result.details, "Provider transaction: "+snapshot.Reference)
		}
		current := live[i]
		switch current.refundStatus {
		case "", commerceschema.RefundStatusFailed:
		case commerceschema.RefundStatusSuccess:
			if math.IsNaN(current.refundAmount) || math.IsInf(current.refundAmount, 0) || current.refundAmount <= 0 || current.refundAmount > snapshot.Amount || current.refundAt <= 0 {
				return commercialOrder{}, ErrInvoiceOrderUnavailable
			}
			refunded += int64(math.Round(current.refundAmount * scale))
			if current.refundAt > result.refundAt {
				result.refundAt = current.refundAt
			}
			result.refundDetails = append(result.refundDetails, fmt.Sprintf("Confirmed cumulative refund: %s / %s %.*f", snapshot.TradeNo, result.currency, commercialInvoiceDigits(result.currency), current.refundAmount))
			if result.refundStatus == "" {
				result.refundStatus = commerceschema.RefundStatusSuccess
			}
		default:
			result.refundStatus = commerceschema.RefundStatusProcessing
		}
	}
	providerNames := make([]string, 0, len(providers))
	for provider := range providers {
		providerNames = append(providerNames, provider)
	}
	sort.Strings(providerNames)
	result.amount, result.refundAmount = float64(total)/scale, float64(refunded)/scale
	result.provider, result.trade = strings.Join(providerNames, ", "), strings.Join(trades, ", ")
	return result, nil
}
