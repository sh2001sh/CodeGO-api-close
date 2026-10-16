package app

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	"gorm.io/gorm"
)

type invoiceEligibleRow struct {
	InvoiceEligibleOrder
	ID              int
	PaymentProvider string
	PaymentMethod   string
	ProviderPayload string
}

// ListInvoiceEligibleOrdersPage keeps filtering and pagination in the database.
// Currency is still determined from the immutable payment snapshot, not the plan.
func ListInvoiceEligibleOrdersPage(userID int, keyword string, page *platformpagination.PageInfo) (*platformpagination.PageInfo, error) {
	return ListInvoiceEligibleOrdersPageWithContext(context.Background(), userID, keyword, page)
}

// ListInvoiceEligibleOrdersPageWithContext releases database work when a search
// is replaced or its HTTP client disconnects.
func ListInvoiceEligibleOrdersPageWithContext(ctx context.Context, userID int, keyword string, page *platformpagination.PageInfo) (*platformpagination.PageInfo, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}
	if page == nil {
		page = &platformpagination.PageInfo{Page: 1, PageSize: 50}
	}
	if page.PageSize <= 0 {
		page.PageSize = 50
	}
	if page.PageSize > 100 {
		page.PageSize = 100
	}
	if page.Page <= 0 {
		page.Page = 1
	}
	if page.Page > math.MaxInt/page.PageSize {
		page.Page = math.MaxInt / page.PageSize
	}
	db := platformdb.DB.WithContext(ctx)
	query := invoiceEligibleOrdersQuery(db, userID)
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword != "" {
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(keyword) + "%"
		amountText := "CASE WHEN order_amount = CAST(order_amount AS BIGINT) THEN CAST(CAST(order_amount AS BIGINT) AS TEXT) ELSE CAST(order_amount AS TEXT) END"
		if platformdb.DB.Dialector.Name() == "mysql" {
			amountText = "CASE WHEN order_amount = CAST(order_amount AS SIGNED) THEN CAST(CAST(order_amount AS SIGNED) AS CHAR) ELSE CAST(order_amount AS CHAR) END"
		}
		currency := `CASE COALESCE(NULLIF(payment_provider, ''), payment_method, '')
			WHEN 'stripe' THEN 'usd' WHEN 'nowpayments' THEN 'usdt'
			WHEN 'epay' THEN 'cny' WHEN 'xunhu' THEN 'cny' WHEN 'wxpay' THEN 'cny' WHEN 'alipay' THEN 'cny' ELSE '' END`
		condition := "LOWER(trade_no) LIKE ? ESCAPE '!' OR LOWER(order_title) LIKE ? ESCAPE '!' OR " + amountText + " LIKE ? ESCAPE '!' OR (" + currency + ") LIKE ? ESCAPE '!'"
		// Creem's currency requires validating amount/tax fields in its payment
		// JSON. Stream only those snapshots for a search instead of depending on
		// dialect-specific JSON casts or inferring an unverified currency.
		creemIDs, err := invoiceCreemCurrencyMatches(db, userID, keyword)
		if err != nil {
			return nil, err
		}
		if creemIDs != "" {
			condition += " OR (" + creemIDs + ")"
		}
		query = query.Where("("+condition+")", pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, err
	}
	rows := make([]invoiceEligibleRow, 0, page.PageSize)
	if err := query.Order("paid_at desc, source_type asc, id desc").
		Limit(page.PageSize).Offset(page.GetStartIdx()).Find(&rows).Error; err != nil {
		return nil, err
	}
	orders := make([]InvoiceEligibleOrder, 0, len(rows))
	trades := make([]string, 0, len(rows))
	for _, row := range rows {
		trades = append(trades, row.TradeNo)
	}
	requested := make(map[string]bool)
	if len(trades) > 0 {
		var items []commerceschema.InvoiceRequestItem
		if err := db.Select("source_type, trade_no").Where("user_id = ? AND trade_no IN ?", userID, trades).Find(&items).Error; err != nil {
			return nil, err
		}
		var requests []commerceschema.InvoiceRequest
		if err := db.Select("source_type, trade_no").Where("user_id = ? AND trade_no IN ?", userID, trades).Find(&requests).Error; err != nil {
			return nil, err
		}
		for _, item := range items {
			requested[invoiceOrderKey(item.SourceType, item.TradeNo)] = true
		}
		for _, request := range requests {
			requested[invoiceOrderKey(request.SourceType, request.TradeNo)] = true
		}
	}
	for _, row := range rows {
		order := row.InvoiceEligibleOrder
		order.Currency = commercialPaymentCurrency(row.PaymentProvider, row.PaymentMethod, row.ProviderPayload, order.OrderAmount)
		order.Requested = requested[invoiceOrderKey(order.SourceType, order.TradeNo)]
		orders = append(orders, order)
	}
	page.SetTotal(int(total))
	page.SetItems(orders)
	return page, nil
}

func invoiceEligibleOrdersQuery(db *gorm.DB, userID int) *gorm.DB {
	// Materialize the current user's subscription trade set once. A correlated
	// NOT EXISTS can rescan an empty subscription table for every top-up. COALESCE
	// matches the legacy Go string key semantics for nullable historical values.
	return db.Table(`(?) AS invoice_orders`, db.Raw(`
		SELECT 'subscription' AS source_type, s.id, s.trade_no, COALESCE(p.title, '套餐订单') AS order_title,
			s.money AS order_amount, s.complete_time AS paid_at, s.refund_status,
			s.payment_provider, s.payment_method, s.provider_payload
		FROM subscription_orders AS s LEFT JOIN subscription_plans AS p ON p.id = s.plan_id
		WHERE s.user_id = ? AND s.status = ? AND s.money > 0
		UNION ALL
		SELECT 'topup' AS source_type, t.id, t.trade_no, '钱包充值' AS order_title,
			t.money AS order_amount, t.complete_time AS paid_at, t.refund_status,
			t.payment_provider, t.payment_method, t.provider_payload
		FROM top_ups AS t
		WHERE t.user_id = ? AND t.status = ? AND t.money > 0
		AND COALESCE(t.trade_no, '') NOT IN (SELECT COALESCE(mirror.trade_no, '') FROM subscription_orders AS mirror
			WHERE mirror.user_id = ? AND mirror.status = ? AND mirror.money > 0)
	`, userID, constant.TopUpStatusSuccess, userID, constant.TopUpStatusSuccess, userID, constant.TopUpStatusSuccess))
}

func invoiceCreemCurrencyMatches(db *gorm.DB, userID int, keyword string) (string, error) {
	rows, err := invoiceEligibleOrdersQuery(db, userID).
		Select("source_type, id, order_amount, payment_provider, payment_method, provider_payload").
		Where("COALESCE(NULLIF(payment_provider, ''), payment_method, '') = ?", commerceschema.PaymentProviderCreem).Rows()
	if err != nil {
		return "", err
	}
	defer rows.Close()
	matches := map[string][]string{}
	for rows.Next() {
		var row invoiceEligibleRow
		if err := db.ScanRows(rows, &row); err != nil {
			return "", err
		}
		currency := commercialPaymentCurrency(row.PaymentProvider, row.PaymentMethod, row.ProviderPayload, row.OrderAmount)
		if strings.Contains(strings.ToLower(currency), keyword) {
			// IDs come from integer database columns; decimal literals avoid SQL
			// parameter limits when a currency matches many historical snapshots.
			matches[row.SourceType] = append(matches[row.SourceType], strconv.Itoa(row.ID))
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	var conditions []string
	for _, source := range []string{commerceschema.InvoiceSourceSubscription, commerceschema.InvoiceSourceTopUp} {
		if ids := matches[source]; len(ids) > 0 {
			conditions = append(conditions, "(source_type = '"+source+"' AND id IN ("+strings.Join(ids, ",")+"))")
		}
	}
	return strings.Join(conditions, " OR "), nil
}
