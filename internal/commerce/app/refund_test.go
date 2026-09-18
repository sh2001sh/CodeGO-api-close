package app

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRefundableTopupUsesFundingLotQuotaUnits(t *testing.T) {
	originalDB := platformdb.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })
	require.NoError(t, db.AutoMigrate(
		&commerceschema.TopUp{},
		&billingschema.BillingAccount{},
		&billingschema.FundingLot{},
	))

	const userID = 5722
	const tradeNo = "refund-quota-units"
	account := billingschema.BillingAccount{AccountID: "refund-wallet", OwnerType: "user", OwnerID: userID, AccountType: "claude_wallet", QuotaUnit: "quota"}
	require.NoError(t, db.Create(&account).Error)
	require.NoError(t, db.Create(&billingschema.FundingLot{
		AccountID:       account.AccountID,
		Source:          billingschema.FundingSourceTopup,
		OriginalAmount:  50_000_000,
		RemainingAmount: 50_000_000,
		IdempotencyKey:  fmt.Sprintf("topup:%s:unified", tradeNo),
	}).Error)
	order := commerceschema.TopUp{UserId: userID, TradeNo: tradeNo, Amount: 100, Money: 100, ProviderPayload: `{"trade_no":"provider-order"}`}

	item := refundableTopup(userID, &order)

	require.EqualValues(t, 50_000_000, item.TotalQuota)
	require.Zero(t, item.UsedQuota)
	require.EqualValues(t, 50_000_000, item.RemainingQuota)
	require.Equal(t, 100.0, item.GrossRefund)
	require.Equal(t, 2.0, item.FeeAmount)
	require.Equal(t, 98.0, item.RefundAmount)
	require.True(t, item.Refundable)
}

func TestRefundMoneyDeductsTwoPercentFee(t *testing.T) {
	gross, fee, net := refundMoney(169, 90, 100)
	require.Equal(t, 152.1, gross)
	require.Equal(t, 3.04, fee)
	require.Equal(t, 149.06, net)
}

func TestRefundMoneyRejectsEmptyQuota(t *testing.T) {
	gross, fee, net := refundMoney(100, 0, 50_000_000)
	require.Zero(t, gross)
	require.Zero(t, fee)
	require.Zero(t, net)
}
