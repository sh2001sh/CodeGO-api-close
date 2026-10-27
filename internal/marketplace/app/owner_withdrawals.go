package app

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	gatewayfiles "github.com/sh2001sh/new-api/internal/gateway/files"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformencodingx "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	OwnerWithdrawalMinimumCredits  int64 = 100
	OwnerWithdrawalRateCNYCents    int64 = 95
	ownerWithdrawalDefaultPageSize       = 20
	ownerWithdrawalMaximumPageSize       = 100
)

type OwnerWithdrawalCreateInput struct {
	OperationID     string `json:"operation_id"`
	Credits         int64  `json:"credits"`
	PaymentMethod   string `json:"payment_method"`
	PayeeName       string `json:"payee_name"`
	PaymentAccount  string `json:"payment_account"`
	PaymentBankName string `json:"payment_bank_name"`
	PaymentQRCodeID string `json:"payment_qr_code_id"`
}

type OwnerWithdrawalPaymentInput struct {
	PaymentMethod   string `json:"payment_method"`
	PayeeName       string `json:"payee_name"`
	PaymentAccount  string `json:"payment_account"`
	PaymentBankName string `json:"payment_bank_name"`
	PaymentQRCodeID string `json:"payment_qr_code_id"`
}

type AdminOwnerWithdrawalActionInput struct {
	Status            string `json:"status"`
	AdminNote         string `json:"admin_note"`
	TransferReference string `json:"transfer_reference"`
}

type OwnerWithdrawalPaymentDetails struct {
	Account  string `json:"account"`
	BankName string `json:"bank_name,omitempty"`
	QRCodeID string `json:"qr_code_id,omitempty"`
}

type OwnerWithdrawalView struct {
	ID                    string     `json:"id"`
	OwnerUserID           int        `json:"owner_user_id,omitempty"`
	OwnerExternalID       string     `json:"owner_external_id,omitempty"`
	RequestedCredits      int64      `json:"requested_credits"`
	RequestedQuota        int64      `json:"requested_quota"`
	RateCNYCentsPerCredit int64      `json:"rate_cny_cents_per_credit"`
	PayoutAmountCents     int64      `json:"payout_amount_cents"`
	PaymentMethod         string     `json:"payment_method"`
	PayeeName             string     `json:"payee_name"`
	PaymentDetailsTail    string     `json:"payment_details_tail"`
	PaymentAccount        string     `json:"payment_account,omitempty"`
	PaymentBankName       string     `json:"payment_bank_name,omitempty"`
	HasPaymentQRCode      bool       `json:"has_payment_qr_code"`
	PaymentQRCodeURL      string     `json:"payment_qr_code_url,omitempty"`
	Status                string     `json:"status"`
	AdminNote             string     `json:"admin_note,omitempty"`
	TransferReference     string     `json:"transfer_reference,omitempty"`
	PaidBy                int        `json:"paid_by,omitempty"`
	PaidAt                *time.Time `json:"paid_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	FailureMessage        string     `json:"failure_message,omitempty"`
}

type OwnerWithdrawalOverview struct {
	ReclaimableQuota          int64 `json:"reclaimable_quota"`
	ReclaimableCredits        int64 `json:"reclaimable_credits"`
	ReclaimableAmountCNYCents int64 `json:"reclaimable_amount_cny_cents"`
	QuotaPerCredit            int64 `json:"quota_per_credit"`
	RateCNYCentsPerCredit     int64 `json:"rate_cny_cents_per_credit"`
	MinimumCredits            int64 `json:"minimum_credits"`
}

type OwnerWithdrawalList struct {
	Items    []OwnerWithdrawalView `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

type AdminOwnerWithdrawalQuery struct {
	Status   string
	Page     int
	PageSize int
}

func GetOwnerWithdrawalOverview(ownerUserID int) (*OwnerWithdrawalOverview, error) {
	quotaPerCredit, err := ownerWithdrawalQuotaPerCredit()
	if err != nil {
		return nil, err
	}
	quota, err := ownerWithdrawalReclaimableQuota(ownerUserID)
	if err != nil {
		return nil, err
	}
	credits := quota / quotaPerCredit
	return &OwnerWithdrawalOverview{
		ReclaimableQuota: quota, ReclaimableCredits: credits,
		ReclaimableAmountCNYCents: credits * OwnerWithdrawalRateCNYCents,
		QuotaPerCredit:            quotaPerCredit, RateCNYCentsPerCredit: OwnerWithdrawalRateCNYCents,
		MinimumCredits: OwnerWithdrawalMinimumCredits,
	}, nil
}

func CreateOwnerWithdrawal(ownerUserID int, input OwnerWithdrawalCreateInput) (*OwnerWithdrawalView, error) {
	input.OperationID = strings.TrimSpace(input.OperationID)
	if ownerUserID <= 0 {
		return nil, errors.New("invalid owner")
	}
	if err := validateOwnerWithdrawalOperationID(input.OperationID); err != nil {
		return nil, err
	}
	quotaPerCredit, err := ownerWithdrawalQuotaPerCredit()
	if err != nil {
		return nil, err
	}
	if input.Credits < OwnerWithdrawalMinimumCredits {
		return nil, fmt.Errorf("每次至少回收 %d credits", OwnerWithdrawalMinimumCredits)
	}
	if input.Credits > math.MaxInt64/quotaPerCredit || input.Credits > math.MaxInt64/OwnerWithdrawalRateCNYCents {
		return nil, errors.New("回收金额过大")
	}
	details, normalized, err := normalizeOwnerWithdrawalPayment(ownerUserID, OwnerWithdrawalPaymentInput{
		PaymentMethod: input.PaymentMethod, PayeeName: input.PayeeName,
		PaymentAccount: input.PaymentAccount, PaymentBankName: input.PaymentBankName,
		PaymentQRCodeID: input.PaymentQRCodeID,
	})
	if err != nil {
		return nil, err
	}
	requestedQuota := input.Credits * quotaPerCredit
	detailsJSON, err := platformencodingx.Marshal(details)
	if err != nil {
		return nil, err
	}
	ciphertext, err := platformsecurity.EncryptSecret(string(detailsJSON))
	if err != nil {
		return nil, fmt.Errorf("encrypt withdrawal payment details: %w", err)
	}
	fingerprintPayload, err := platformencodingx.Marshal(struct {
		OwnerUserID int
		Credits     int64
		Payment     OwnerWithdrawalPaymentInput
	}{ownerUserID, input.Credits, normalized})
	if err != nil {
		return nil, err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(fingerprintPayload))
	reclaimOperationID := fmt.Sprintf("%x", sha256.Sum256([]byte("owner-withdrawal:"+input.OperationID)))
	record := marketplaceschema.OwnerWithdrawal{
		ID: input.OperationID, OwnerUserID: ownerUserID, ReclaimOperationID: reclaimOperationID,
		RequestFingerprint: fingerprint, RequestedCredits: input.Credits,
		RequestedQuota: requestedQuota, RateCNYCentsPerCredit: OwnerWithdrawalRateCNYCents,
		PayoutAmountCents: input.Credits * OwnerWithdrawalRateCNYCents,
		PaymentMethod:     normalized.PaymentMethod, PayeeName: normalized.PayeeName,
		PaymentDetailsCiphertext: ciphertext, PaymentDetailsTail: paymentDetailsTail(normalized.PaymentAccount),
		Status: marketplaceschema.OwnerWithdrawalStatusPendingTransfer,
	}
	inserted := false
	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := marketplacesettlement.LockOwnerReclaimsTx(tx, []int{ownerUserID}); err != nil {
			return err
		}
		var existing marketplaceschema.OwnerWithdrawal
		if err := tx.First(&existing, "id = ?", input.OperationID).Error; err == nil {
			if existing.OwnerUserID != ownerUserID || existing.RequestFingerprint != fingerprint {
				return errors.New("操作标识已用于其他回收申请")
			}
			record = existing
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		available, err := ownerWithdrawalReclaimableQuotaTx(tx, ownerUserID)
		if err != nil {
			return err
		}
		if requestedQuota > available {
			return errors.New("申请金额超过当前可回收收益")
		}
		reservationID, err := billingapp.ReserveMarketplaceOwnerWithdrawalTx(tx, ownerUserID, requestedQuota, reclaimOperationID)
		if err != nil {
			return err
		}
		record.ReservationID = reservationID
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("操作标识已用于其他回收申请")
		}
		inserted = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if inserted {
		invalidateOwnerWithdrawalWalletCaches(ownerUserID)
		invalidateAdminMarketplaceStatsCache()
	}
	view := ownerWithdrawalView(record, false)
	return &view, nil
}

func ListOwnerWithdrawals(ownerUserID, page, pageSize int) (*OwnerWithdrawalList, error) {
	if ownerUserID <= 0 {
		return nil, errors.New("invalid owner")
	}
	if err := reconcileOwnerWithdrawalStatuses(ownerUserID); err != nil {
		return nil, err
	}
	page, pageSize = normalizeOwnerWithdrawalPage(page, pageSize)
	query := platformdb.DB.Model(&marketplaceschema.OwnerWithdrawal{}).Where("owner_user_id = ?", ownerUserID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var records []marketplaceschema.OwnerWithdrawal
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]OwnerWithdrawalView, 0, len(records))
	for _, record := range records {
		items = append(items, ownerWithdrawalView(record, false))
	}
	return &OwnerWithdrawalList{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func UpdateOwnerWithdrawalPayment(ownerUserID int, withdrawalID string, input OwnerWithdrawalPaymentInput) (*OwnerWithdrawalView, error) {
	details, normalized, err := normalizeOwnerWithdrawalPayment(ownerUserID, input)
	if err != nil {
		return nil, err
	}
	raw, err := platformencodingx.Marshal(details)
	if err != nil {
		return nil, err
	}
	ciphertext, err := platformsecurity.EncryptSecret(string(raw))
	if err != nil {
		return nil, err
	}
	var record marketplaceschema.OwnerWithdrawal
	err = platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, "id = ? AND owner_user_id = ?", strings.TrimSpace(withdrawalID), ownerUserID).Error; err != nil {
			return err
		}
		if record.Status != marketplaceschema.OwnerWithdrawalStatusNeedsUpdate {
			return errors.New("仅资料需修改的申请可以更新收款信息")
		}
		return tx.Model(&record).Updates(map[string]any{
			"payment_method": normalized.PaymentMethod, "payee_name": normalized.PayeeName,
			"payment_details_ciphertext": ciphertext, "payment_details_tail": paymentDetailsTail(normalized.PaymentAccount),
			"status": marketplaceschema.OwnerWithdrawalStatusPendingTransfer, "admin_note": "",
		}).Error
	})
	if err != nil {
		return nil, err
	}
	record.PaymentMethod = normalized.PaymentMethod
	record.PayeeName = normalized.PayeeName
	record.PaymentDetailsTail = paymentDetailsTail(normalized.PaymentAccount)
	record.Status = marketplaceschema.OwnerWithdrawalStatusPendingTransfer
	record.AdminNote = ""
	view := ownerWithdrawalView(record, false)
	return &view, nil
}

func ListAdminOwnerWithdrawals(input AdminOwnerWithdrawalQuery) (*OwnerWithdrawalList, error) {
	if input.Status != "" && !validOwnerWithdrawalStatus(input.Status) {
		return nil, errors.New("无效的回收申请状态")
	}
	if err := reconcileOwnerWithdrawalStatuses(0); err != nil {
		return nil, err
	}
	page, pageSize := normalizeOwnerWithdrawalPage(input.Page, input.PageSize)
	query := platformdb.DB.Model(&marketplaceschema.OwnerWithdrawal{})
	if input.Status != "" {
		query = query.Where("status = ?", input.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	var records []marketplaceschema.OwnerWithdrawal
	if err := query.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&records).Error; err != nil {
		return nil, err
	}
	ownerIDs := make([]int, 0, len(records))
	for _, record := range records {
		ownerIDs = append(ownerIDs, record.OwnerUserID)
	}
	externalIDs, err := ownerExternalIDs(ownerIDs)
	if err != nil {
		return nil, err
	}
	items := make([]OwnerWithdrawalView, 0, len(records))
	for _, record := range records {
		view, err := adminOwnerWithdrawalView(record)
		if err != nil {
			return nil, err
		}
		view.OwnerExternalID = externalIDs[record.OwnerUserID]
		items = append(items, view)
	}
	return &OwnerWithdrawalList{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func UpdateAdminOwnerWithdrawal(adminUserID int, withdrawalID string, input AdminOwnerWithdrawalActionInput) (*OwnerWithdrawalView, error) {
	if adminUserID <= 0 {
		return nil, errors.New("invalid administrator")
	}
	input.Status = strings.TrimSpace(input.Status)
	input.AdminNote = strings.TrimSpace(input.AdminNote)
	input.TransferReference = strings.TrimSpace(input.TransferReference)
	if utf8.RuneCountInString(input.AdminNote) > 1000 || utf8.RuneCountInString(input.TransferReference) > 128 {
		return nil, errors.New("备注或转账流水号过长")
	}
	if input.Status != marketplaceschema.OwnerWithdrawalStatusNeedsUpdate && input.Status != marketplaceschema.OwnerWithdrawalStatusPaid {
		return nil, errors.New("管理员只能标记资料需修改或已回收")
	}
	if input.Status == marketplaceschema.OwnerWithdrawalStatusNeedsUpdate && input.AdminNote == "" {
		return nil, errors.New("请填写需要修改的收款资料")
	}
	var record marketplaceschema.OwnerWithdrawal
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, "id = ?", strings.TrimSpace(withdrawalID)).Error; err != nil {
			return err
		}
		if record.Status == marketplaceschema.OwnerWithdrawalStatusPaid {
			if input.Status == record.Status {
				return nil
			}
			return errors.New("该申请已完成回收，不能再次修改")
		}
		if record.Status != marketplaceschema.OwnerWithdrawalStatusPendingTransfer {
			return errors.New("当前申请状态不能执行此操作")
		}
		updates := map[string]any{"status": input.Status, "admin_note": input.AdminNote}
		if input.Status == marketplaceschema.OwnerWithdrawalStatusPaid {
			if record.ReservationID != "" {
				if err := billingapp.SettleMarketplaceOwnerWithdrawalTx(tx, record.ReservationID, record.OwnerUserID, adminUserID, record.RequestedQuota, record.ReclaimOperationID); err != nil {
					return err
				}
				if _, err := marketplacesettlement.CreateChargedIncomeReclaimTaskTx(tx, record.OwnerUserID, record.RequestedQuota, record.ReclaimOperationID); err != nil {
					return err
				}
			}
			now := time.Now().UTC()
			updates["paid_by"] = adminUserID
			updates["paid_at"] = now
			if input.TransferReference != "" {
				updates["transfer_reference"] = input.TransferReference
				record.TransferReference = input.TransferReference
			}
			record.PaidBy = adminUserID
			record.PaidAt = &now
		}
		if err := tx.Model(&record).Updates(updates).Error; err != nil {
			return err
		}
		record.Status = input.Status
		record.AdminNote = input.AdminNote
		return nil
	})
	if err != nil {
		return nil, err
	}
	if input.Status == marketplaceschema.OwnerWithdrawalStatusPaid && record.ReservationID != "" {
		invalidateOwnerWithdrawalWalletCaches(record.OwnerUserID, adminUserID)
	}
	view, err := adminOwnerWithdrawalView(record)
	return &view, err
}

func GetOwnerWithdrawalQRCode(withdrawalID string, viewerUserID int, admin bool) (*gatewayschema.UserFile, error) {
	var record marketplaceschema.OwnerWithdrawal
	query := platformdb.DB.Where("id = ?", strings.TrimSpace(withdrawalID))
	if !admin {
		query = query.Where("owner_user_id = ?", viewerUserID)
	}
	if err := query.First(&record).Error; err != nil {
		return nil, err
	}
	details, err := decryptOwnerWithdrawalPayment(record)
	if err != nil {
		return nil, err
	}
	if record.PaymentMethod != marketplaceschema.OwnerWithdrawalMethodQRCode || details.QRCodeID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	file, err := gatewayfiles.Get(details.QRCodeID, record.OwnerUserID, false)
	if err != nil {
		return nil, err
	}
	if err := gatewayfiles.MarkUsed(file); err != nil {
		return nil, err
	}
	return file, nil
}

func ownerWithdrawalReclaimableQuota(ownerUserID int) (int64, error) {
	return ownerWithdrawalReclaimableQuotaDB(platformdb.DB, ownerUserID, false)
}

func ownerWithdrawalReclaimableQuotaTx(tx *gorm.DB, ownerUserID int) (int64, error) {
	return ownerWithdrawalReclaimableQuotaDB(tx, ownerUserID, true)
}

func ownerWithdrawalReclaimableQuotaDB(db *gorm.DB, ownerUserID int, lockBalance bool) (int64, error) {
	if ownerUserID <= 0 {
		return 0, errors.New("invalid owner")
	}
	facts, err := incomeFacts(db, 0, 0)
	if err != nil {
		return 0, err
	}
	var released int64
	err = facts.Where("owner_user_id = ?", ownerUserID).
		Select("COALESCE(SUM(released_income), 0)").Scan(&released).Error
	if err != nil {
		return 0, err
	}
	var held int64
	if err := db.Model(&marketplaceschema.OwnerWithdrawal{}).
		Where("owner_user_id = ? AND reservation_id <> ? AND status IN ?", ownerUserID, "", []string{
			marketplaceschema.OwnerWithdrawalStatusPendingTransfer,
			marketplaceschema.OwnerWithdrawalStatusNeedsUpdate,
		}).Select("COALESCE(SUM(requested_quota), 0)").Scan(&held).Error; err != nil {
		return 0, err
	}
	released = max(int64(0), released-held)
	var outstandingAudits int64
	if err := db.Table(marketplaceschema.OwnerWithdrawal{}.TableName()+" AS w").
		Joins("JOIN "+marketplaceschema.IncomeReclaim{}.TableName()+" AS r ON r.id = w.reclaim_operation_id").
		Where("w.owner_user_id = ? AND w.reservation_id <> ? AND w.status = ? AND r.status <> ?", ownerUserID, "", marketplaceschema.OwnerWithdrawalStatusPaid, "completed").
		Count(&outstandingAudits).Error; err != nil {
		return 0, err
	}
	if outstandingAudits > 0 {
		return 0, nil
	}
	var balances map[int]int64
	if lockBalance {
		balances, err = billingapp.LockMarketplaceReclaimBalancesTx(db, []int{ownerUserID})
	} else {
		balances, err = billingapp.GetUserClaudeWalletQuotas([]int{ownerUserID})
	}
	if err != nil {
		return 0, err
	}
	return max(int64(0), min(released, balances[ownerUserID])), nil
}

func invalidateOwnerWithdrawalWalletCaches(userIDs ...int) {
	for _, userID := range userIDs {
		if err := platformcache.DeleteUserCache(userID); err != nil {
			platformobservability.SysError(fmt.Sprintf("invalidate owner withdrawal wallet cache for user %d: %v", userID, err))
		}
	}
}

func reconcileOwnerWithdrawalStatuses(ownerUserID int) error {
	query := platformdb.DB.Where("status = ?", marketplaceschema.OwnerWithdrawalStatusReclaiming)
	if ownerUserID > 0 {
		query = query.Where("owner_user_id = ?", ownerUserID)
	}
	var records []marketplaceschema.OwnerWithdrawal
	if err := query.Find(&records).Error; err != nil {
		return err
	}
	for _, record := range records {
		task, err := marketplacesettlement.GetIncomeReclaimTask(record.ReclaimOperationID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		updates := map[string]any{}
		switch task.Status {
		case "charged", "completed":
			updates["status"] = marketplaceschema.OwnerWithdrawalStatusPendingTransfer
			updates["failure_message"] = ""
		case "failed":
			updates["status"] = marketplaceschema.OwnerWithdrawalStatusFailed
			updates["failure_message"] = task.ErrorMessage
		}
		if len(updates) > 0 {
			if err := platformdb.DB.Model(&record).Updates(updates).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeOwnerWithdrawalPayment(ownerUserID int, input OwnerWithdrawalPaymentInput) (OwnerWithdrawalPaymentDetails, OwnerWithdrawalPaymentInput, error) {
	input.PaymentMethod = strings.TrimSpace(input.PaymentMethod)
	input.PayeeName = strings.TrimSpace(input.PayeeName)
	input.PaymentAccount = strings.TrimSpace(input.PaymentAccount)
	input.PaymentBankName = strings.TrimSpace(input.PaymentBankName)
	input.PaymentQRCodeID = strings.TrimSpace(input.PaymentQRCodeID)
	if input.PaymentMethod != marketplaceschema.OwnerWithdrawalMethodAlipay && input.PaymentMethod != marketplaceschema.OwnerWithdrawalMethodBankCard && input.PaymentMethod != marketplaceschema.OwnerWithdrawalMethodQRCode {
		return OwnerWithdrawalPaymentDetails{}, input, errors.New("收款方式仅支持支付宝、银行卡或收款码")
	}
	if utf8.RuneCountInString(input.PayeeName) < 2 || utf8.RuneCountInString(input.PayeeName) > 100 {
		return OwnerWithdrawalPaymentDetails{}, input, errors.New("收款人姓名长度必须为 2 到 100 个字符")
	}
	if input.PaymentMethod == marketplaceschema.OwnerWithdrawalMethodQRCode {
		if err := validateOwnerWithdrawalQRCode(ownerUserID, input.PaymentQRCodeID); err != nil {
			return OwnerWithdrawalPaymentDetails{}, input, err
		}
		input.PaymentAccount = ""
		input.PaymentBankName = ""
	} else {
		accountLength := utf8.RuneCountInString(input.PaymentAccount)
		if accountLength < 3 || accountLength > 200 {
			return OwnerWithdrawalPaymentDetails{}, input, errors.New("收款账号长度必须为 3 到 200 个字符")
		}
		input.PaymentQRCodeID = ""
	}
	if input.PaymentMethod == marketplaceschema.OwnerWithdrawalMethodBankCard {
		bankLength := utf8.RuneCountInString(input.PaymentBankName)
		if bankLength < 2 || bankLength > 100 {
			return OwnerWithdrawalPaymentDetails{}, input, errors.New("银行卡收款必须填写开户银行")
		}
	} else if input.PaymentMethod != marketplaceschema.OwnerWithdrawalMethodQRCode {
		input.PaymentBankName = ""
	}
	return OwnerWithdrawalPaymentDetails{Account: input.PaymentAccount, BankName: input.PaymentBankName, QRCodeID: input.PaymentQRCodeID}, input, nil
}

func validateOwnerWithdrawalQRCode(ownerUserID int, fileID string) error {
	if fileID == "" {
		return errors.New("请上传收款码")
	}
	file, err := gatewayfiles.Get(fileID, ownerUserID, false)
	if err != nil {
		return errors.New("收款码不存在或已失效")
	}
	if file.Purpose != gatewayfiles.PurposeMarketplaceOwnerWithdrawalQRCode || file.Size <= 0 || file.Size > 2<<20 {
		return errors.New("收款码文件无效")
	}
	content, err := gatewayfiles.ReadContent(file)
	if err != nil || len(content) == 0 {
		return errors.New("收款码文件无法读取")
	}
	detected := http.DetectContentType(content[:min(len(content), 512)])
	if detected != "image/png" && detected != "image/jpeg" && detected != "image/webp" {
		return errors.New("收款码仅支持 PNG、JPEG 或 WebP 图片")
	}
	return nil
}

func adminOwnerWithdrawalView(record marketplaceschema.OwnerWithdrawal) (OwnerWithdrawalView, error) {
	view := ownerWithdrawalView(record, true)
	details, err := decryptOwnerWithdrawalPayment(record)
	if err != nil {
		return view, err
	}
	view.PaymentAccount = details.Account
	view.PaymentBankName = details.BankName
	view.HasPaymentQRCode = details.QRCodeID != ""
	if view.HasPaymentQRCode {
		view.PaymentQRCodeURL = "/api/marketplace/admin/owner-withdrawals/" + record.ID + "/qr-code"
	}
	view.FailureMessage = record.FailureMessage
	return view, nil
}

func decryptOwnerWithdrawalPayment(record marketplaceschema.OwnerWithdrawal) (OwnerWithdrawalPaymentDetails, error) {
	raw, err := platformsecurity.DecryptSecret(record.PaymentDetailsCiphertext)
	if err != nil {
		return OwnerWithdrawalPaymentDetails{}, fmt.Errorf("decrypt withdrawal %s payment details: %w", record.ID, err)
	}
	var details OwnerWithdrawalPaymentDetails
	if err := platformencodingx.Unmarshal([]byte(raw), &details); err != nil {
		return OwnerWithdrawalPaymentDetails{}, fmt.Errorf("decode withdrawal %s payment details: %w", record.ID, err)
	}
	return details, nil
}

func ownerWithdrawalView(record marketplaceschema.OwnerWithdrawal, admin bool) OwnerWithdrawalView {
	view := OwnerWithdrawalView{
		ID: record.ID, RequestedCredits: record.RequestedCredits, RequestedQuota: record.RequestedQuota,
		RateCNYCentsPerCredit: record.RateCNYCentsPerCredit, PayoutAmountCents: record.PayoutAmountCents,
		PaymentMethod: record.PaymentMethod, PayeeName: record.PayeeName,
		PaymentDetailsTail: record.PaymentDetailsTail, Status: record.Status,
		AdminNote: record.AdminNote, TransferReference: record.TransferReference,
		PaidAt: record.PaidAt, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
	view.HasPaymentQRCode = record.PaymentMethod == marketplaceschema.OwnerWithdrawalMethodQRCode
	if admin {
		view.OwnerUserID = record.OwnerUserID
		view.PaidBy = record.PaidBy
	}
	if record.Status == marketplaceschema.OwnerWithdrawalStatusFailed && !admin {
		view.FailureMessage = "收益回收失败，请刷新可回收金额后重试"
	}
	return view
}

func ownerWithdrawalQuotaPerCredit() (int64, error) {
	if platformruntime.QuotaPerUnit <= 0 || math.Trunc(platformruntime.QuotaPerUnit) != platformruntime.QuotaPerUnit || platformruntime.QuotaPerUnit > math.MaxInt64 {
		return 0, errors.New("系统 credit 额度换算配置无效")
	}
	return int64(platformruntime.QuotaPerUnit), nil
}

func validateOwnerWithdrawalOperationID(value string) error {
	if len(value) < 8 || len(value) > 64 {
		return errors.New("操作标识长度必须为 8 到 64 个字符")
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return errors.New("操作标识包含无效字符")
	}
	return nil
}

func paymentDetailsTail(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= 4 {
		return string(runes)
	}
	return string(runes[len(runes)-4:])
}

func normalizeOwnerWithdrawalPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = ownerWithdrawalDefaultPageSize
	}
	if pageSize > ownerWithdrawalMaximumPageSize {
		pageSize = ownerWithdrawalMaximumPageSize
	}
	return page, pageSize
}

func validOwnerWithdrawalStatus(status string) bool {
	switch status {
	case marketplaceschema.OwnerWithdrawalStatusReclaiming,
		marketplaceschema.OwnerWithdrawalStatusPendingTransfer,
		marketplaceschema.OwnerWithdrawalStatusNeedsUpdate,
		marketplaceschema.OwnerWithdrawalStatusPaid,
		marketplaceschema.OwnerWithdrawalStatusFailed:
		return true
	default:
		return false
	}
}
