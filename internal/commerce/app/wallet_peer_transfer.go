package app

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sh2001sh/new-api/constant"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const walletTransferMaxPageSize = 50

type WalletTransferRecipient struct {
	ExternalId        string `json:"external_id"`
	DisplayNameMasked string `json:"display_name_masked"`
}

type CreateWalletTransferRequest struct {
	RecipientExternalId string `json:"recipient_external_id"`
	AmountQuota         int64  `json:"amount_quota"`
	PaymentPassword     string `json:"payment_password"`
	RequestId           string `json:"request_id"`
}

type WalletTransferHistoryItem struct {
	Id                      int    `json:"id"`
	RequestId               string `json:"request_id"`
	Direction               string `json:"direction"`
	CounterpartyExternalId  string `json:"counterparty_external_id"`
	CounterpartyDisplayName string `json:"counterparty_display_name_masked"`
	AmountQuota             int64  `json:"amount_quota"`
	BalanceAfter            int64  `json:"balance_after"`
	Status                  string `json:"status"`
	CreatedAt               int64  `json:"created_at"`
}

type WalletTransferHistoryPage struct {
	Page     int                         `json:"page"`
	PageSize int                         `json:"page_size"`
	Total    int64                       `json:"total"`
	Items    []WalletTransferHistoryItem `json:"items"`
}

type WalletTransferOverview struct {
	QuotaPerUSD int64                          `json:"quota_per_usd"`
	MinQuota    int64                          `json:"min_quota"`
	Balance     int64                          `json:"balance"`
	Security    WalletTransferSecurityOverview `json:"security"`
	History     WalletTransferHistoryPage      `json:"history"`
}

// LookupWalletTransferRecipient resolves only public, masked recipient data.
func LookupWalletTransferRecipient(senderUserID int, externalID string) (*WalletTransferRecipient, error) {
	externalID = strings.ToUpper(strings.TrimSpace(externalID))
	if senderUserID <= 0 || len(externalID) != identityschema.ExternalUserIDLength {
		return nil, commerceschema.ErrWalletTransferRecipientNotFound
	}
	var user identityschema.User
	err := platformdb.DB.Select("id, external_id, display_name, username, status").Where("external_id = ?", externalID).First(&user).Error
	if err != nil || user.Status != constant.UserStatusEnabled {
		return nil, commerceschema.ErrWalletTransferRecipientNotFound
	}
	if user.Id == senderUserID {
		return nil, commerceschema.ErrWalletTransferSelf
	}
	return recipientFromUser(&user), nil
}

func BuildWalletTransferOverview(user *identityschema.User, page, pageSize int) (*WalletTransferOverview, error) {
	if user == nil || user.Id <= 0 {
		return nil, commerceschema.ErrWalletTransferInvalid
	}
	balance, err := billingapp.GetUserWalletQuota(user.Id)
	if err != nil {
		return nil, err
	}
	security, err := GetWalletTransferSecurityOverview(user.Id, user.Password != "")
	if err != nil {
		return nil, err
	}
	history, err := ListWalletTransfers(user.Id, page, pageSize)
	if err != nil {
		return nil, err
	}
	return &WalletTransferOverview{
		QuotaPerUSD: int64(platformruntime.QuotaPerUnit),
		MinQuota:    minimumWalletTransferQuota(),
		Balance:     int64(balance),
		Security:    *security,
		History:     *history,
	}, nil
}

// TransferWalletQuota atomically debits the sender and credits the recipient.
func TransferWalletQuota(senderUserID int, req CreateWalletTransferRequest) (*WalletTransferHistoryItem, error) {
	req.RecipientExternalId = strings.ToUpper(strings.TrimSpace(req.RecipientExternalId))
	req.RequestId = strings.TrimSpace(req.RequestId)
	if !validWalletTransferRequest(senderUserID, req) {
		return nil, commerceschema.ErrWalletTransferInvalid
	}
	if err := verifyWalletTransferPassword(senderUserID, req.PaymentPassword); err != nil {
		return nil, err
	}

	var transfer commerceschema.WalletTransfer
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		recipient, err := loadTransferRecipientTx(tx, req.RecipientExternalId)
		if err != nil {
			return err
		}
		if recipient.Id == senderUserID {
			return commerceschema.ErrWalletTransferSelf
		}
		users, err := lockTransferUsers(tx, senderUserID, recipient.Id)
		if err != nil {
			return err
		}
		sender := users[senderUserID]
		recipient = users[recipient.Id]
		if existing, found, err := findExistingWalletTransferTx(tx, senderUserID, recipient.Id, req); err != nil {
			return err
		} else if found {
			transfer = *existing
			return nil
		}
		return executeWalletTransferTx(tx, sender, recipient, req, &transfer)
	})
	if err != nil {
		return nil, mapWalletTransferError(err)
	}
	_ = identitystore.InvalidateUserCache(senderUserID)
	_ = identitystore.InvalidateUserCache(transfer.RecipientUserId)
	item := walletTransferHistoryItem(transfer, senderUserID)
	return &item, nil
}

func ListWalletTransfers(userID, page, pageSize int) (*WalletTransferHistoryPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > walletTransferMaxPageSize {
		pageSize = walletTransferMaxPageSize
	}
	query := platformdb.DB.Model(&commerceschema.WalletTransfer{}).
		Where("sender_user_id = ? OR recipient_user_id = ?", userID, userID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var transfers []commerceschema.WalletTransfer
	if err := query.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&transfers).Error; err != nil {
		return nil, err
	}
	items := make([]WalletTransferHistoryItem, 0, len(transfers))
	for _, transfer := range transfers {
		items = append(items, walletTransferHistoryItem(transfer, userID))
	}
	return &WalletTransferHistoryPage{Page: page, PageSize: pageSize, Total: total, Items: items}, nil
}

func validWalletTransferRequest(senderUserID int, req CreateWalletTransferRequest) bool {
	minimumQuota := minimumWalletTransferQuota()
	return senderUserID > 0 && len(req.RecipientExternalId) == identityschema.ExternalUserIDLength &&
		req.AmountQuota >= minimumQuota && req.AmountQuota%minimumQuota == 0 && req.AmountQuota <= int64(^uint(0)>>1) &&
		req.RequestId != "" && len(req.RequestId) <= 128 && req.PaymentPassword != ""
}

func minimumWalletTransferQuota() int64 {
	minimum := int64(platformruntime.QuotaPerUnit) / 100
	if minimum < 1 {
		return 1
	}
	return minimum
}

func loadTransferRecipientTx(tx *gorm.DB, externalID string) (*identityschema.User, error) {
	var user identityschema.User
	if err := tx.Where("external_id = ? AND status = ?", externalID, constant.UserStatusEnabled).First(&user).Error; err != nil {
		return nil, commerceschema.ErrWalletTransferRecipientNotFound
	}
	return &user, nil
}

func lockTransferUsers(tx *gorm.DB, senderID, recipientID int) (map[int]*identityschema.User, error) {
	var rows []*identityschema.User
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", []int{senderID, recipientID}).Order("id asc").Find(&rows).Error
	if err != nil || len(rows) != 2 {
		return nil, commerceschema.ErrWalletTransferRecipientNotFound
	}
	users := make(map[int]*identityschema.User, len(rows))
	for _, user := range rows {
		users[user.Id] = user
	}
	return users, nil
}

func findExistingWalletTransferTx(tx *gorm.DB, senderID, recipientID int, req CreateWalletTransferRequest) (*commerceschema.WalletTransfer, bool, error) {
	var existing commerceschema.WalletTransfer
	err := tx.Where("request_id = ?", req.RequestId).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if existing.SenderUserId != senderID || existing.RecipientUserId != recipientID || existing.AmountQuota != req.AmountQuota {
		return nil, false, commerceschema.ErrWalletTransferInvalid
	}
	return &existing, true, nil
}

func executeWalletTransferTx(tx *gorm.DB, sender, recipient *identityschema.User, req CreateWalletTransferRequest, transfer *commerceschema.WalletTransfer) error {
	senderBalance, recipientBalance, err := synchronizeTransferBalancesTx(tx, sender, recipient)
	if err != nil {
		return err
	}
	if senderBalance < int(req.AmountQuota) {
		return commerceschema.ErrWalletTransferInsufficientBalance
	}
	operationID := "wallet-transfer:" + req.RequestId
	if err := billingapp.DebitWalletQuotaTxWithReason(tx, sender.Id, int(req.AmountQuota), operationID+":debit", "wallet_peer_transfer_debit"); err != nil {
		return err
	}
	if err := billingapp.CreditWalletQuotaTx(tx, recipient.Id, int(req.AmountQuota), operationID+":credit", "wallet_peer_transfer_credit"); err != nil {
		return err
	}
	*transfer = newWalletTransfer(sender, recipient, req, int64(senderBalance)-req.AmountQuota, int64(recipientBalance)+req.AmountQuota)
	if err := tx.Create(transfer).Error; err != nil {
		return err
	}
	return recordWalletTransferAuditTx(tx, transfer)
}

func synchronizeTransferBalancesTx(tx *gorm.DB, sender, recipient *identityschema.User) (int, int, error) {
	senderBalance, _, err := billingapp.SynchronizeWalletQuotaProjectionsTx(tx, sender.Id, sender.Quota, sender.ClaudeQuota)
	if err != nil {
		return 0, 0, err
	}
	recipientBalance, _, err := billingapp.SynchronizeWalletQuotaProjectionsTx(tx, recipient.Id, recipient.Quota, recipient.ClaudeQuota)
	return senderBalance, recipientBalance, err
}

func newWalletTransfer(sender, recipient *identityschema.User, req CreateWalletTransferRequest, senderAfter, recipientAfter int64) commerceschema.WalletTransfer {
	return commerceschema.WalletTransfer{
		RequestId: req.RequestId, SenderUserId: sender.Id, RecipientUserId: recipient.Id,
		SenderExternalId: sender.ExternalId, RecipientExternalId: recipient.ExternalId,
		SenderDisplayNameMasked:    maskWalletTransferName(displayNameForTransfer(sender)),
		RecipientDisplayNameMasked: maskWalletTransferName(displayNameForTransfer(recipient)),
		AmountQuota:                req.AmountQuota, SenderBalanceAfter: senderAfter, RecipientBalanceAfter: recipientAfter,
	}
}

func recordWalletTransferAuditTx(tx *gorm.DB, transfer *commerceschema.WalletTransfer) error {
	amount := float64(transfer.AmountQuota) / float64(platformruntime.QuotaPerUnit)
	if err := auditapp.RecordLogTx(tx, transfer.SenderUserId, auditschema.LogTypeManage, fmt.Sprintf("向用户 %s 转出普通额度 $%.2f，转账记录 %d", transfer.RecipientExternalId, amount, transfer.Id)); err != nil {
		return err
	}
	return auditapp.RecordLogTx(tx, transfer.RecipientUserId, auditschema.LogTypeManage, fmt.Sprintf("收到用户 %s 转入普通额度 $%.2f，转账记录 %d", transfer.SenderExternalId, amount, transfer.Id))
}

func walletTransferHistoryItem(transfer commerceschema.WalletTransfer, userID int) WalletTransferHistoryItem {
	item := WalletTransferHistoryItem{Id: transfer.Id, RequestId: transfer.RequestId, AmountQuota: transfer.AmountQuota, Status: transfer.Status, CreatedAt: transfer.CreatedAt}
	if transfer.SenderUserId == userID {
		item.Direction = "outgoing"
		item.CounterpartyExternalId = transfer.RecipientExternalId
		item.CounterpartyDisplayName = transfer.RecipientDisplayNameMasked
		item.BalanceAfter = transfer.SenderBalanceAfter
	} else {
		item.Direction = "incoming"
		item.CounterpartyExternalId = transfer.SenderExternalId
		item.CounterpartyDisplayName = transfer.SenderDisplayNameMasked
		item.BalanceAfter = transfer.RecipientBalanceAfter
	}
	return item
}

func recipientFromUser(user *identityschema.User) *WalletTransferRecipient {
	return &WalletTransferRecipient{ExternalId: user.ExternalId, DisplayNameMasked: maskWalletTransferName(displayNameForTransfer(user))}
}

func displayNameForTransfer(user *identityschema.User) string {
	if strings.TrimSpace(user.DisplayName) != "" {
		return strings.TrimSpace(user.DisplayName)
	}
	return strings.TrimSpace(user.Username)
}

func maskWalletTransferName(value string) string {
	if value == "" {
		return "***"
	}
	runes := []rune(value)
	if len(runes) == 1 {
		return string(runes[0]) + "***"
	}
	if len(runes) == 2 {
		return string(runes[0]) + "*"
	}
	maskedCount := utf8.RuneCountInString(value) - 2
	if maskedCount > 4 {
		maskedCount = 4
	}
	return string(runes[0]) + strings.Repeat("*", maskedCount) + string(runes[len(runes)-1])
}

func mapWalletTransferError(err error) error {
	if errors.Is(err, billingdomain.ErrInsufficientBalance) {
		return commerceschema.ErrWalletTransferInsufficientBalance
	}
	return err
}
