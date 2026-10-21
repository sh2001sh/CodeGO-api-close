package app

import (
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	"gorm.io/gorm"
)

// Only the current page's invoice IDs are expanded. A member must reflect refund
// activity on the whole invoice, including members on another search/page.
func annotateCommercialInvoiceOrders(db *gorm.DB, userID int, orders []InvoiceEligibleOrder) error {
	if len(orders) == 0 {
		return nil
	}
	trades := make([]string, len(orders))
	for i, order := range orders {
		trades[i] = order.TradeNo
	}
	var items []commerceschema.InvoiceRequestItem
	if err := db.Select("invoice_id, source_type, trade_no").Where("user_id = ? AND trade_no IN ?", userID, trades).Find(&items).Error; err != nil {
		return err
	}
	ids := make([]int64, 0, len(items))
	memberIDs := make(map[string]int64, len(items))
	for _, item := range items {
		ids = append(ids, item.InvoiceID)
		memberIDs[invoiceOrderKey(item.SourceType, item.TradeNo)] = item.InvoiceID
	}
	var requests []commerceschema.InvoiceRequest
	if err := db.Select("id, source_type, trade_no, order_count, invoice_number").Where("user_id = ? AND (id IN ? OR trade_no IN ?)", userID, ids, trades).Find(&requests).Error; err != nil {
		return err
	}
	byID := make(map[int64]commerceschema.InvoiceRequest, len(requests))
	for _, request := range requests {
		byID[request.ID] = request
		memberIDs[invoiceOrderKey(request.SourceType, request.TradeNo)] = request.ID
	}
	type refundState struct {
		InvoiceID int64
		State     int
	}
	var refunds []refundState
	if len(ids) > 0 {
		if err := db.Raw(`SELECT invoice_id, MAX(CASE COALESCE(refund_status, '') WHEN '' THEN 0 WHEN 'failed' THEN 0 WHEN 'success' THEN 1 ELSE 2 END) AS state FROM (
			SELECT i.invoice_id, t.refund_status FROM invoice_request_items i JOIN top_ups t ON t.user_id = i.user_id AND t.trade_no = i.trade_no
			WHERE i.user_id = ? AND i.source_type = 'topup' AND i.invoice_id IN ?
			UNION ALL
			SELECT i.invoice_id, s.refund_status FROM invoice_request_items i JOIN subscription_orders s ON s.user_id = i.user_id AND s.trade_no = i.trade_no
			WHERE i.user_id = ? AND i.source_type = 'subscription' AND i.invoice_id IN ?
		) invoice_refunds GROUP BY invoice_id`, userID, ids, userID, ids).Scan(&refunds).Error; err != nil {
			return err
		}
	}
	states := make(map[int64]string, len(refunds))
	for _, row := range refunds {
		if row.State == 1 {
			states[row.InvoiceID] = commerceschema.RefundStatusSuccess
		} else if row.State == 2 {
			states[row.InvoiceID] = commerceschema.RefundStatusProcessing
		}
	}
	for i := range orders {
		id, found := memberIDs[invoiceOrderKey(orders[i].SourceType, orders[i].TradeNo)]
		orders[i].Requested = found
		if found {
			request := byID[id]
			orders[i].InvoiceOrderCount, orders[i].InvoiceNumber = request.OrderCount, request.InvoiceNumber
			orders[i].InvoiceRefundStatus = states[id]
		}
	}
	return nil
}
