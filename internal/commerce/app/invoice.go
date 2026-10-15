package app

import (
	"errors"
	"net/mail"
	"sort"
	"strings"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformpagination "github.com/sh2001sh/new-api/internal/platform/pagination"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
)

const invoiceRequestMaxLength = 255

var (
	ErrInvoiceOrderUnavailable = errors.New("该订单暂不可申请发票")
	ErrInvoiceAlreadyRequested = errors.New("该订单已提交发票申请")
)

type InvoiceEligibleOrder struct {
	SourceType  string  `json:"source_type"`
	TradeNo     string  `json:"trade_no"`
	OrderTitle  string  `json:"order_title"`
	OrderAmount float64 `json:"order_amount"`
	Currency    string  `json:"currency"`
	PaidAt      int64   `json:"paid_at"`
	Requested   bool    `json:"requested"`
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

type InvoiceOrderInput struct {
	SourceType string `json:"source_type"`
	TradeNo    string `json:"trade_no"`
}

type UpdateInvoiceRequestInput struct {
	Status        string `json:"status"`
	InvoiceNumber string `json:"invoice_number"`
	AdminNote     string `json:"admin_note"`
}

// ListInvoiceEligibleOrders returns paid orders that the user may invoice.
func ListInvoiceEligibleOrders(userID int) ([]InvoiceEligibleOrder, error) {
	if userID <= 0 {
		return nil, errors.New("invalid user id")
	}

	requested, err := invoiceRequestedOrders(userID)
	if err != nil {
		return nil, err
	}
	orders := make([]InvoiceEligibleOrder, 0)
	subscriptionTrades := make(map[string]struct{})
	var subscriptions []commerceschema.SubscriptionOrder
	if err := platformdb.DB.Where("user_id = ? AND status = ? AND money > 0", userID, constant.TopUpStatusSuccess).
		Order("complete_time desc, id desc").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	for _, order := range subscriptions {
		subscriptionTrades[order.TradeNo] = struct{}{}
		title := "套餐订单"
		currency := "CNY"
		var plan commerceschema.SubscriptionPlan
		if planErr := platformdb.DB.Select("title, currency").First(&plan, order.PlanId).Error; planErr == nil {
			title = plan.Title
			if strings.TrimSpace(plan.Currency) != "" {
				currency = plan.Currency
			}
		}
		orders = append(orders, InvoiceEligibleOrder{
			SourceType: commerceschema.InvoiceSourceSubscription,
			TradeNo:    order.TradeNo, OrderTitle: title, OrderAmount: order.Money,
			Currency: currency, PaidAt: order.CompleteTime,
			Requested: requested[invoiceOrderKey(commerceschema.InvoiceSourceSubscription, order.TradeNo)],
		})
	}

	var topups []commerceschema.TopUp
	if err := platformdb.DB.Where("user_id = ? AND status = ? AND money > 0", userID, constant.TopUpStatusSuccess).
		Order("complete_time desc, id desc").Find(&topups).Error; err != nil {
		return nil, err
	}
	for _, topup := range topups {
		if _, duplicatedSubscription := subscriptionTrades[topup.TradeNo]; duplicatedSubscription {
			continue
		}
		orders = append(orders, InvoiceEligibleOrder{
			SourceType: commerceschema.InvoiceSourceTopUp,
			TradeNo:    topup.TradeNo, OrderTitle: "钱包充值", OrderAmount: topup.Money,
			Currency: "CNY", PaidAt: topup.CompleteTime,
			Requested: requested[invoiceOrderKey(commerceschema.InvoiceSourceTopUp, topup.TradeNo)],
		})
	}
	sort.Slice(orders, func(i, j int) bool { return orders[i].PaidAt > orders[j].PaidAt })
	return orders, nil
}

func ListUserInvoiceRequests(userID int, page *platformpagination.PageInfo) (*platformpagination.PageInfo, error) {
	var requests []commerceschema.InvoiceRequest
	var total int64
	query := platformdb.DB.Model(&commerceschema.InvoiceRequest{}).Where("user_id = ?", userID)
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	if err := query.Order("id desc").Limit(page.GetPageSize()).Offset(page.GetStartIdx()).Find(&requests).Error; err != nil {
		return nil, err
	}
	page.SetTotal(int(total))
	page.SetItems(requests)
	return page, nil
}

func ListAdminInvoiceRequests(status string, page *platformpagination.PageInfo) (*platformpagination.PageInfo, error) {
	var requests []commerceschema.InvoiceRequest
	var total int64
	query := platformdb.DB.Model(&commerceschema.InvoiceRequest{})
	if status = strings.TrimSpace(status); status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	if err := query.Order("status asc, id asc").Limit(page.GetPageSize()).Offset(page.GetStartIdx()).Find(&requests).Error; err != nil {
		return nil, err
	}
	page.SetTotal(int(total))
	page.SetItems(requests)
	return page, nil
}

// CreateInvoiceRequest validates paid orders inside a transaction and reserves them for one application.
func CreateInvoiceRequest(userID int, input CreateInvoiceRequestInput) (*commerceschema.InvoiceRequest, error) {
	if err := validateInvoiceCreateInput(&input); err != nil {
		return nil, err
	}
	request := &commerceschema.InvoiceRequest{}
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		orders := make([]*InvoiceEligibleOrder, 0, len(input.Orders))
		seen := make(map[string]struct{}, len(input.Orders))
		currency := ""
		for _, orderInput := range input.Orders {
			key := invoiceOrderKey(orderInput.SourceType, orderInput.TradeNo)
			if _, ok := seen[key]; ok {
				return ErrInvoiceOrderUnavailable
			}
			seen[key] = struct{}{}
			eligible, err := loadInvoiceEligibleOrderTx(tx, userID, orderInput.SourceType, orderInput.TradeNo)
			if err != nil {
				return err
			}
			var existing commerceschema.InvoiceRequestItem
			if err := tx.Where("source_type = ? AND trade_no = ?", orderInput.SourceType, orderInput.TradeNo).First(&existing).Error; err == nil {
				return ErrInvoiceAlreadyRequested
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if currency == "" {
				currency = eligible.Currency
			} else if currency != eligible.Currency {
				return errors.New("合并开票的订单币种必须一致")
			}
			orders = append(orders, eligible)
		}
		if len(orders) == 0 {
			return errors.New("请选择可开票订单")
		}
		amount := 0.0
		for _, order := range orders {
			amount += order.OrderAmount
		}
		tradeNo := input.Orders[0].TradeNo
		if len(orders) > 1 {
			tradeNo = "batch-" + platformruntime.GetUUID()
		}
		*request = commerceschema.InvoiceRequest{
			UserID: userID, SourceType: input.Orders[0].SourceType, TradeNo: tradeNo,
			OrderAmount: amount, Currency: currency, OrderTitle: orders[0].OrderTitle,
			OrderCount:  len(orders),
			InvoiceType: input.InvoiceType, Title: input.Title, TaxNumber: input.TaxNumber,
			Email: input.Email, Remark: input.Remark, Status: commerceschema.InvoiceStatusPending,
		}
		if len(orders) > 1 {
			request.SourceType = commerceschema.InvoiceSourceBatch
			request.OrderTitle = "合并开票"
		}
		if err := tx.Create(request).Error; err != nil {
			return err
		}
		for index, order := range orders {
			itemInput := input.Orders[index]
			if err := tx.Create(&commerceschema.InvoiceRequestItem{
				InvoiceID: request.ID, UserID: userID, SourceType: itemInput.SourceType,
				TradeNo: itemInput.TradeNo, OrderAmount: order.OrderAmount,
				Currency: order.Currency, OrderTitle: order.OrderTitle, PaidAt: order.PaidAt,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return request, nil
}

func UpdateAdminInvoiceRequest(id int64, adminID int, input UpdateInvoiceRequestInput) (*commerceschema.InvoiceRequest, error) {
	if id <= 0 || adminID <= 0 {
		return nil, errors.New("invalid invoice request")
	}
	if err := validateInvoiceAdminInput(&input); err != nil {
		return nil, err
	}
	var result commerceschema.InvoiceRequest
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Set("gorm:query_option", "FOR UPDATE").First(&result, id).Error; err != nil {
			return err
		}
		result.Status, result.InvoiceNumber = input.Status, input.InvoiceNumber
		result.AdminNote, result.HandledBy = input.AdminNote, adminID
		if input.Status == commerceschema.InvoiceStatusIssued && result.IssuedAt == 0 {
			result.IssuedAt = platformruntime.GetTimestamp()
		}
		return tx.Save(&result).Error
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func invoiceRequestedOrders(userID int) (map[string]bool, error) {
	var items []commerceschema.InvoiceRequestItem
	if err := platformdb.DB.Select("source_type, trade_no").Where("user_id = ?", userID).Find(&items).Error; err != nil {
		return nil, err
	}
	var requests []commerceschema.InvoiceRequest
	if err := platformdb.DB.Select("source_type, trade_no").Where("user_id = ?", userID).Find(&requests).Error; err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(items)+len(requests))
	for _, item := range items {
		result[invoiceOrderKey(item.SourceType, item.TradeNo)] = true
	}
	for _, request := range requests {
		if request.SourceType == commerceschema.InvoiceSourceBatch {
			continue
		}
		result[invoiceOrderKey(request.SourceType, request.TradeNo)] = true
	}
	return result, nil
}

func loadInvoiceEligibleOrderTx(tx *gorm.DB, userID int, sourceType, tradeNo string) (*InvoiceEligibleOrder, error) {
	switch sourceType {
	case commerceschema.InvoiceSourceSubscription:
		var order commerceschema.SubscriptionOrder
		if err := tx.Where("user_id = ? AND trade_no = ? AND status = ? AND money > 0", userID, tradeNo, constant.TopUpStatusSuccess).First(&order).Error; err != nil {
			return nil, ErrInvoiceOrderUnavailable
		}
		title, currency := "套餐订单", "CNY"
		var plan commerceschema.SubscriptionPlan
		if err := tx.First(&plan, order.PlanId).Error; err == nil {
			title = plan.Title
			if strings.TrimSpace(plan.Currency) != "" {
				currency = plan.Currency
			}
		}
		return &InvoiceEligibleOrder{SourceType: sourceType, TradeNo: tradeNo, OrderTitle: title, OrderAmount: order.Money, Currency: currency, PaidAt: order.CompleteTime}, nil
	case commerceschema.InvoiceSourceTopUp:
		var subscriptionCount int64
		if err := tx.Model(&commerceschema.SubscriptionOrder{}).Where("trade_no = ?", tradeNo).Count(&subscriptionCount).Error; err != nil {
			return nil, err
		}
		if subscriptionCount > 0 {
			return nil, ErrInvoiceOrderUnavailable
		}
		var topup commerceschema.TopUp
		if err := tx.Where("user_id = ? AND trade_no = ? AND status = ? AND money > 0", userID, tradeNo, constant.TopUpStatusSuccess).First(&topup).Error; err != nil {
			return nil, ErrInvoiceOrderUnavailable
		}
		return &InvoiceEligibleOrder{SourceType: sourceType, TradeNo: tradeNo, OrderTitle: "钱包充值", OrderAmount: topup.Money, Currency: "CNY", PaidAt: topup.CompleteTime}, nil
	default:
		return nil, ErrInvoiceOrderUnavailable
	}
}

func validateInvoiceCreateInput(input *CreateInvoiceRequestInput) error {
	if input == nil {
		return errors.New("invalid invoice request")
	}
	input.SourceType, input.TradeNo = strings.TrimSpace(input.SourceType), strings.TrimSpace(input.TradeNo)
	if len(input.Orders) == 0 && input.SourceType != "" && input.TradeNo != "" {
		input.Orders = []InvoiceOrderInput{{SourceType: input.SourceType, TradeNo: input.TradeNo}}
	}
	for index := range input.Orders {
		input.Orders[index].SourceType = strings.TrimSpace(input.Orders[index].SourceType)
		input.Orders[index].TradeNo = strings.TrimSpace(input.Orders[index].TradeNo)
	}
	input.InvoiceType, input.Title = strings.TrimSpace(input.InvoiceType), strings.TrimSpace(input.Title)
	input.TaxNumber, input.Email, input.Remark = strings.TrimSpace(input.TaxNumber), strings.TrimSpace(input.Email), strings.TrimSpace(input.Remark)
	if len(input.Orders) == 0 {
		return errors.New("请选择可开票订单")
	}
	for _, order := range input.Orders {
		if (order.SourceType != commerceschema.InvoiceSourceTopUp && order.SourceType != commerceschema.InvoiceSourceSubscription) || order.TradeNo == "" {
			return errors.New("请选择可开票订单")
		}
	}
	if input.InvoiceType != commerceschema.InvoiceTypePersonal && input.InvoiceType != commerceschema.InvoiceTypeEnterprise {
		return errors.New("请选择发票类型")
	}
	if input.Title == "" || len([]rune(input.Title)) > invoiceRequestMaxLength {
		return errors.New("发票抬头不合法")
	}
	if input.InvoiceType == commerceschema.InvoiceTypeEnterprise && len([]rune(input.TaxNumber)) < 8 {
		return errors.New("企业发票需要填写正确税号")
	}
	if _, err := mail.ParseAddress(input.Email); err != nil {
		return errors.New("接收邮箱不合法")
	}
	if len([]rune(input.Remark)) > 500 {
		return errors.New("备注不能超过 500 个字符")
	}
	return nil
}

func validateInvoiceAdminInput(input *UpdateInvoiceRequestInput) error {
	if input == nil {
		return errors.New("invalid invoice request")
	}
	input.Status, input.InvoiceNumber = strings.TrimSpace(input.Status), strings.TrimSpace(input.InvoiceNumber)
	input.AdminNote = strings.TrimSpace(input.AdminNote)
	if input.Status != commerceschema.InvoiceStatusIssued && input.Status != commerceschema.InvoiceStatusRejected {
		return errors.New("请选择处理结果")
	}
	if input.Status == commerceschema.InvoiceStatusIssued && input.InvoiceNumber == "" {
		return errors.New("已开具发票必须填写发票号码")
	}
	if input.Status == commerceschema.InvoiceStatusRejected && input.AdminNote == "" {
		return errors.New("驳回申请需要填写原因")
	}
	if len([]rune(input.AdminNote)) > 1000 || len([]rune(input.InvoiceNumber)) > 128 {
		return errors.New("处理信息过长")
	}
	return nil
}

func invoiceOrderKey(sourceType, tradeNo string) string { return sourceType + ":" + tradeNo }
