package app

import (
	"strings"
	"testing"

	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	marketplacesettlement "github.com/sh2001sh/new-api/internal/marketplace/settlement"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOwnerWithdrawalUsesFixedManualPayoutRateAndHidesPaymentAccount(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	migrateOwnerWithdrawalTestSchema(t, db)
	unit := int64(platformruntime.QuotaPerUnit)
	require.NoError(t, db.Create([]identityschema.User{
		{Id: 1, ExternalId: "ADMIN1", Username: "admin", AffCode: "admin"},
		{Id: 42, ExternalId: "OWNER42", Username: "withdraw-owner", AffCode: "owner42", ClaudeQuota: int(150 * unit)},
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		ID: "withdraw-income", RequestID: "withdraw-income", GroupID: "g", OwnerUserID: 42,
		OwnerNetAmount: 150 * unit, Status: "released",
	}).Error)
	result, err := CreateOwnerWithdrawal(42, OwnerWithdrawalCreateInput{
		OperationID: "withdrawal-idempotency-0001", Credits: 100,
		PaymentMethod: marketplaceschema.OwnerWithdrawalMethodAlipay,
		PayeeName:     "张三", PaymentAccount: "owner@example.com",
	})
	require.NoError(t, err)
	require.Equal(t, marketplaceschema.OwnerWithdrawalStatusPendingTransfer, result.Status)
	require.EqualValues(t, 100, result.RequestedCredits)
	require.EqualValues(t, 100*unit, result.RequestedQuota)
	require.EqualValues(t, 95, result.RateCNYCentsPerCredit)
	require.EqualValues(t, 9500, result.PayoutAmountCents)
	require.Equal(t, ".com", result.PaymentDetailsTail)
	require.Empty(t, result.PaymentAccount)

	var stored marketplaceschema.OwnerWithdrawal
	require.NoError(t, db.First(&stored, "id = ?", result.ID).Error)
	require.NotEmpty(t, stored.ReservationID)
	require.NotContains(t, stored.PaymentDetailsCiphertext, "owner@example.com")
	plaintext, err := platformsecurity.DecryptSecret(stored.PaymentDetailsCiphertext)
	require.NoError(t, err)
	require.Contains(t, plaintext, "owner@example.com")

	admin, err := ListAdminOwnerWithdrawals(AdminOwnerWithdrawalQuery{})
	require.NoError(t, err)
	require.Equal(t, "owner@example.com", admin.Items[0].PaymentAccount)
	require.Equal(t, "OWNER42", admin.Items[0].OwnerExternalID)

	ownerBeforePaid := ownerWithdrawalWalletSnapshot(t, db, 42)
	require.EqualValues(t, 50*unit, ownerBeforePaid.AvailableBalance)
	require.EqualValues(t, 100*unit, ownerBeforePaid.ReservedBalance)
	require.Zero(t, ownerBeforePaid.ConsumedTotal)
	var owner identityschema.User
	require.NoError(t, db.First(&owner, 42).Error)
	require.Equal(t, int(150*unit), owner.ClaudeQuota, "freezing must not permanently debit the legacy wallet projection")
	frozen, err := GetOwnerIncomeFreeze(42)
	require.NoError(t, err)
	require.EqualValues(t, 100*unit, frozen.FrozenAmount)

	paid, err := UpdateAdminOwnerWithdrawal(1, result.ID, AdminOwnerWithdrawalActionInput{
		Status: marketplaceschema.OwnerWithdrawalStatusPaid,
	})
	require.NoError(t, err)
	require.Equal(t, marketplaceschema.OwnerWithdrawalStatusPaid, paid.Status)
	require.NotNil(t, paid.PaidAt)
	ownerAfterPaid := ownerWithdrawalWalletSnapshot(t, db, 42)
	require.EqualValues(t, 50*unit, ownerAfterPaid.AvailableBalance)
	require.Zero(t, ownerAfterPaid.ReservedBalance)
	require.EqualValues(t, 100*unit, ownerAfterPaid.ConsumedTotal)
	require.NoError(t, db.First(&owner, 42).Error)
	require.Equal(t, int(50*unit), owner.ClaudeQuota)
	adminAfterPaid := ownerWithdrawalWalletSnapshot(t, db, 1)
	require.EqualValues(t, 100*unit, adminAfterPaid.AvailableBalance)

	repeated, err := UpdateAdminOwnerWithdrawal(1, result.ID, AdminOwnerWithdrawalActionInput{
		Status: marketplaceschema.OwnerWithdrawalStatusPaid,
	})
	require.NoError(t, err)
	require.Equal(t, marketplaceschema.OwnerWithdrawalStatusPaid, repeated.Status)
	require.Equal(t, ownerAfterPaid, ownerWithdrawalWalletSnapshot(t, db, 42))
	require.Equal(t, adminAfterPaid, ownerWithdrawalWalletSnapshot(t, db, 1))

	var reclaim marketplaceschema.IncomeReclaim
	require.NoError(t, db.First(&reclaim, "id = ?", stored.ReclaimOperationID).Error)
	require.EqualValues(t, 100*unit, reclaim.Amount)
	require.Equal(t, "charged", reclaim.Status)
	var settlement marketplaceschema.Settlement
	require.NoError(t, db.First(&settlement, "id = ?", "withdraw-income").Error)
	require.Zero(t, settlement.ReclaimedAmount, "confirmation must not wait for settlement-history audit")

	_, err = marketplacesettlement.ProcessDirectIncomeReclaimTask(stored.ReclaimOperationID)
	require.NoError(t, err)
	require.NoError(t, db.First(&reclaim, "id = ?", stored.ReclaimOperationID).Error)
	require.Equal(t, "completed", reclaim.Status)
	require.NoError(t, db.First(&settlement, "id = ?", "withdraw-income").Error)
	require.EqualValues(t, 100*unit, settlement.ReclaimedAmount)
}

func TestOwnerWithdrawalRejectsSmallAmountAndPreventsDuplicateDebit(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	migrateOwnerWithdrawalTestSchema(t, db)
	unit := int64(platformruntime.QuotaPerUnit)
	require.NoError(t, db.Create(&identityschema.User{Id: 42, Username: "owner", ClaudeQuota: int(200 * unit)}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		ID: "income", RequestID: "income", GroupID: "g", OwnerUserID: 42,
		OwnerNetAmount: 200 * unit, Status: "released",
	}).Error)
	base := OwnerWithdrawalCreateInput{
		OperationID: "withdrawal-idempotency-0002", Credits: 99,
		PaymentMethod: marketplaceschema.OwnerWithdrawalMethodAlipay,
		PayeeName:     "张三", PaymentAccount: "owner@example.com",
	}
	_, err := CreateOwnerWithdrawal(42, base)
	require.ErrorContains(t, err, "至少回收 100")
	require.EqualValues(t, 0, ownerWithdrawalReservationCount(t, db))

	base.Credits = 100
	first, err := CreateOwnerWithdrawal(42, base)
	require.NoError(t, err)
	second, err := CreateOwnerWithdrawal(42, base)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.EqualValues(t, 1, ownerWithdrawalReservationCount(t, db))

	base.Credits = 101
	_, err = CreateOwnerWithdrawal(42, base)
	require.ErrorContains(t, err, "操作标识已用于其他回收申请")
	require.EqualValues(t, 1, ownerWithdrawalReservationCount(t, db))
}

func TestOwnerWithdrawalCapsReclaimableAmountAtSpendableWalletBalance(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	migrateOwnerWithdrawalTestSchema(t, db)
	unit := int64(platformruntime.QuotaPerUnit)
	require.NoError(t, db.Create(&identityschema.User{
		Id: 42, Username: "wallet-capped-owner", ClaudeQuota: int(120 * unit),
	}).Error)
	require.NoError(t, db.Create(&marketplaceschema.Settlement{
		ID: "wallet-capped-income", RequestID: "wallet-capped-income", GroupID: "g", OwnerUserID: 42,
		OwnerNetAmount: 500 * unit, Status: "released",
	}).Error)

	overview, err := GetOwnerWithdrawalOverview(42)
	require.NoError(t, err)
	require.EqualValues(t, 120, overview.ReclaimableCredits)
	require.EqualValues(t, 120*unit, overview.ReclaimableQuota)

	input := OwnerWithdrawalCreateInput{
		OperationID: "withdrawal-wallet-cap-0001", Credits: 121,
		PaymentMethod: marketplaceschema.OwnerWithdrawalMethodAlipay,
		PayeeName:     "张三", PaymentAccount: "owner@example.com",
	}
	_, err = CreateOwnerWithdrawal(42, input)
	require.ErrorContains(t, err, "申请金额超过当前可回收收益")
	require.EqualValues(t, 0, ownerWithdrawalReservationCount(t, db))

	input.OperationID = "withdrawal-wallet-cap-0002"
	input.Credits = 120
	result, err := CreateOwnerWithdrawal(42, input)
	require.NoError(t, err)
	require.EqualValues(t, 120, result.RequestedCredits)
	snapshot := ownerWithdrawalWalletSnapshot(t, db, 42)
	require.Zero(t, snapshot.AvailableBalance)
	require.EqualValues(t, 120*unit, snapshot.ReservedBalance)
}

func TestOwnerWithdrawalPaymentUpdateChecksOwnershipAndAdminPaymentReference(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.OwnerWithdrawal{}))
	details, err := platformsecurity.EncryptSecret(`{"account":"old@example.com"}`)
	require.NoError(t, err)
	record := marketplaceschema.OwnerWithdrawal{
		ID: "withdrawal-update-0001", OwnerUserID: 42, ReclaimOperationID: strings.Repeat("a", 64),
		RequestFingerprint: strings.Repeat("b", 64), RequestedCredits: 100, RequestedQuota: 50_000_000,
		RateCNYCentsPerCredit: 95, PayoutAmountCents: 9500,
		PaymentMethod: marketplaceschema.OwnerWithdrawalMethodAlipay, PayeeName: "张三",
		PaymentDetailsCiphertext: details, PaymentDetailsTail: ".com",
		Status: marketplaceschema.OwnerWithdrawalStatusNeedsUpdate,
	}
	require.NoError(t, db.Create(&record).Error)

	update := OwnerWithdrawalPaymentInput{
		PaymentMethod: marketplaceschema.OwnerWithdrawalMethodBankCard,
		PayeeName:     "张三", PaymentAccount: "6222020000001234", PaymentBankName: "中国工商银行",
	}
	_, err = UpdateOwnerWithdrawalPayment(77, record.ID, update)
	require.Error(t, err)
	updated, err := UpdateOwnerWithdrawalPayment(42, record.ID, update)
	require.NoError(t, err)
	require.Equal(t, marketplaceschema.OwnerWithdrawalStatusPendingTransfer, updated.Status)
	require.Equal(t, "1234", updated.PaymentDetailsTail)
	require.Empty(t, updated.PaymentAccount)

	paid, err := UpdateAdminOwnerWithdrawal(1, record.ID, AdminOwnerWithdrawalActionInput{
		Status: marketplaceschema.OwnerWithdrawalStatusPaid,
	})
	require.NoError(t, err)
	require.Equal(t, marketplaceschema.OwnerWithdrawalStatusPaid, paid.Status)
	require.Empty(t, paid.TransferReference)
	require.NotNil(t, paid.PaidAt)

	repeated, err := UpdateAdminOwnerWithdrawal(1, record.ID, AdminOwnerWithdrawalActionInput{
		Status: marketplaceschema.OwnerWithdrawalStatusPaid, TransferReference: "OTHER",
	})
	require.NoError(t, err)
	require.Empty(t, repeated.TransferReference)
}

func TestOwnerWithdrawalPaidFailureKeepsReservationAndPendingStatus(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	migrateOwnerWithdrawalTestSchema(t, db)
	details, err := platformsecurity.EncryptSecret(`{"account":"owner@example.com"}`)
	require.NoError(t, err)
	record := marketplaceschema.OwnerWithdrawal{
		ID: "withdrawal-missing-reservation", OwnerUserID: 42,
		ReclaimOperationID: strings.Repeat("c", 64), ReservationID: "missing-reservation",
		RequestFingerprint: strings.Repeat("d", 64), RequestedCredits: 100, RequestedQuota: 50_000_000,
		RateCNYCentsPerCredit: 95, PayoutAmountCents: 9500,
		PaymentMethod: marketplaceschema.OwnerWithdrawalMethodAlipay, PayeeName: "张三",
		PaymentDetailsCiphertext: details, PaymentDetailsTail: ".com",
		Status: marketplaceschema.OwnerWithdrawalStatusPendingTransfer,
	}
	require.NoError(t, db.Create(&record).Error)

	_, err = UpdateAdminOwnerWithdrawal(1, record.ID, AdminOwnerWithdrawalActionInput{
		Status: marketplaceschema.OwnerWithdrawalStatusPaid,
	})
	require.Error(t, err)
	require.NoError(t, db.First(&record, "id = ?", record.ID).Error)
	require.Equal(t, marketplaceschema.OwnerWithdrawalStatusPendingTransfer, record.Status)
	require.Nil(t, record.PaidAt)
	var tasks int64
	require.NoError(t, db.Model(&marketplaceschema.IncomeReclaim{}).Count(&tasks).Error)
	require.Zero(t, tasks, "failed payout confirmation must roll back its audit task")
}

func TestOwnerWithdrawalBankPaymentRequiresNameBankAndCardNumber(t *testing.T) {
	base := OwnerWithdrawalPaymentInput{
		PaymentMethod:   marketplaceschema.OwnerWithdrawalMethodBankCard,
		PayeeName:       "张三",
		PaymentAccount:  "6222020000001234",
		PaymentBankName: "中国工商银行",
	}

	missingName := base
	missingName.PayeeName = ""
	_, _, err := normalizeOwnerWithdrawalPayment(42, missingName)
	require.ErrorContains(t, err, "收款人姓名")

	missingCard := base
	missingCard.PaymentAccount = ""
	_, _, err = normalizeOwnerWithdrawalPayment(42, missingCard)
	require.ErrorContains(t, err, "收款账号")

	missingBank := base
	missingBank.PaymentBankName = ""
	_, _, err = normalizeOwnerWithdrawalPayment(42, missingBank)
	require.ErrorContains(t, err, "开户银行")

	details, normalized, err := normalizeOwnerWithdrawalPayment(42, base)
	require.NoError(t, err)
	require.Equal(t, base.PaymentAccount, details.Account)
	require.Equal(t, base.PaymentBankName, details.BankName)
	require.Equal(t, base, normalized)
}

func migrateOwnerWithdrawalTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(
		&identityschema.User{},
		&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{},
		&billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{},
		&billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{},
		&marketplaceschema.Settlement{}, &marketplaceschema.IncomeReclaim{},
		&marketplaceschema.OwnerWithdrawal{},
	))
}

func ownerWithdrawalWalletSnapshot(t *testing.T, db *gorm.DB, userID int) billingschema.BillingBalanceSnapshot {
	t.Helper()
	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_type = ? AND owner_id = ? AND account_type = ?", "user", userID, "claude_wallet").First(&account).Error)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
	return snapshot
}

func ownerWithdrawalReservationCount(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&billingschema.BillingReservation{}).Count(&count).Error)
	return count
}
