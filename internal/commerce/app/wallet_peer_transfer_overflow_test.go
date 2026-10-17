package app

import (
	"context"
	"math"
	"math/big"
	"testing"

	"github.com/sh2001sh/new-api/constant"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestWalletTransferFeeAvoidsIntermediateOverflow(t *testing.T) {
	for _, amount := range []int64{0, 1, 99, 100, 101, 9999, 10000, math.MaxInt64 / 100, math.MaxInt64/100 + 1, math.MaxInt64} {
		expected := new(big.Int).Mul(big.NewInt(amount), big.NewInt(walletTransferFeeBPS))
		expected.Add(expected, big.NewInt(9999)).Div(expected, big.NewInt(10000))
		require.Equal(t, expected.Int64(), walletTransferFee(amount), "amount=%d", amount)
	}
}

func TestWalletTransferRequestIncludesFeeInIntegerLimit(t *testing.T) {
	previousUnit := platformruntime.QuotaPerUnit
	platformruntime.QuotaPerUnit = 100
	t.Cleanup(func() { platformruntime.QuotaPerUnit = previousUnit })
	maxQuota := int64(^uint(0) >> 1)
	limit := new(big.Int).Mul(big.NewInt(maxQuota), big.NewInt(10000))
	limit.Div(limit, big.NewInt(10000+walletTransferFeeBPS))
	request := CreateWalletTransferRequest{RecipientExternalId: "SAFE01", PaymentPassword: "Paypass123", RequestId: "integer-limit"}
	for _, test := range []struct {
		amount int64
		valid  bool
	}{
		{-1, false}, {0, false}, {1, true}, {limit.Int64(), true}, {limit.Int64() + 1, false}, {maxQuota, false}, {math.MaxInt64, false},
	} {
		request.AmountQuota = test.amount
		require.Equal(t, test.valid, validWalletTransferRequest(1, request), "amount=%d", test.amount)
	}
}

func TestTransferWalletQuotaRejectsRecipientOverflowWithoutWrites(t *testing.T) {
	db := setupRedemptionTestDB(t)
	amount := minimumWalletTransferQuota()
	maxQuota := int64(^uint(0) >> 1)
	sender := &identityschema.User{Id: 9941, ExternalId: "OVER01", Username: "overflow-sender", AffCode: "OVER01", Status: constant.UserStatusEnabled, ClaudeQuota: int(amount * 10)}
	recipient := &identityschema.User{Id: 9942, ExternalId: "OVER02", Username: "overflow-recipient", AffCode: "OVER02", Status: constant.UserStatusEnabled, ClaudeQuota: int(maxQuota - amount + 1)}
	require.NoError(t, db.Create(sender).Error)
	require.NoError(t, db.Create(recipient).Error)
	require.NoError(t, ConfigureWalletTransferPassword(sender.Id, "", "Paypass123"))
	request := CreateWalletTransferRequest{RecipientExternalId: recipient.ExternalId, AmountQuota: amount, PaymentPassword: "Paypass123", RequestId: "recipient-overflow"}
	_, err := TransferWalletQuota(context.Background(), sender.Id, request)
	require.ErrorIs(t, err, commerceschema.ErrWalletTransferInvalid)
	var savedSender, savedRecipient identityschema.User
	require.NoError(t, db.First(&savedSender, sender.Id).Error)
	require.NoError(t, db.First(&savedRecipient, recipient.Id).Error)
	require.Equal(t, sender.ClaudeQuota, savedSender.ClaudeQuota)
	require.Equal(t, recipient.ClaudeQuota, savedRecipient.ClaudeQuota)
	for _, model := range []any{&commerceschema.WalletTransfer{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingOutboxEvent{}, &billingschema.FundingLot{}} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.Zero(t, count)
	}
}

func TestTransferWalletQuotaLargeFeeAndExactRecipientLimit(t *testing.T) {
	db := setupRedemptionTestDB(t)
	minimum := minimumWalletTransferQuota()
	// Cross the old fee's intermediate multiplication limit with a valid step.
	amount := (int64(math.MaxInt64)/walletTransferFeeBPS/minimum + 1) * minimum
	maxQuota := int64(^uint(0) >> 1)
	if amount > maxQuota-walletTransferFee(amount) {
		t.Skip("large int64 transfer requires a 64-bit quota integer")
	}
	fee := new(big.Int).Mul(big.NewInt(amount), big.NewInt(walletTransferFeeBPS))
	fee.Add(fee, big.NewInt(9999)).Div(fee, big.NewInt(10000))
	sender := &identityschema.User{Id: 9943, ExternalId: "EDGE01", Username: "large-sender", AffCode: "EDGE01", Status: constant.UserStatusEnabled, ClaudeQuota: int(amount + fee.Int64())}
	recipient := &identityschema.User{Id: 9944, ExternalId: "EDGE02", Username: "limit-recipient", AffCode: "EDGE02", Status: constant.UserStatusEnabled, ClaudeQuota: int(maxQuota - amount)}
	require.NoError(t, db.Create(sender).Error)
	require.NoError(t, db.Create(recipient).Error)
	require.NoError(t, ConfigureWalletTransferPassword(sender.Id, "", "Paypass123"))
	request := CreateWalletTransferRequest{RecipientExternalId: recipient.ExternalId, AmountQuota: amount, PaymentPassword: "Paypass123", RequestId: "exact-recipient-limit"}
	created, err := TransferWalletQuota(context.Background(), sender.Id, request)
	require.NoError(t, err)
	require.Equal(t, fee.Int64(), created.FeeQuota)
	require.Equal(t, amount+fee.Int64(), created.TotalDebitQuota)
	require.Zero(t, created.BalanceAfter)
	replayed, err := TransferWalletQuota(context.Background(), sender.Id, request)
	require.NoError(t, err)
	require.Equal(t, created.Id, replayed.Id)
	var savedSender, savedRecipient identityschema.User
	require.NoError(t, db.First(&savedSender, sender.Id).Error)
	require.NoError(t, db.First(&savedRecipient, recipient.Id).Error)
	require.Zero(t, savedSender.ClaudeQuota)
	require.Equal(t, int(maxQuota), savedRecipient.ClaudeQuota)
	var ledger struct {
		AvailableBalance int64
		GrantedTotal     int64
	}
	require.NoError(t, db.Table("billing_balance_snapshots AS s").
		Select("s.available_balance, s.granted_total").
		Joins("JOIN billing_accounts AS a ON a.account_id = s.account_id").
		Where("a.owner_id = ? AND a.account_type = ?", recipient.Id, "claude_wallet").Scan(&ledger).Error)
	require.Equal(t, maxQuota, ledger.AvailableBalance)
	require.Equal(t, maxQuota, ledger.GrantedTotal)
}

func TestTransferWalletQuotaRejectsLifetimeOverflowAndRollsBackSender(t *testing.T) {
	if int64(^uint(0)>>1) != math.MaxInt64 {
		t.Skip("lifetime int64 quota fixture requires a 64-bit integer")
	}
	db := setupRedemptionTestDB(t)
	sender := identityschema.User{Id: 9947, ExternalId: "LIFE01", Username: "lifetime-sender", AffCode: "LIFE01", Status: constant.UserStatusEnabled, ClaudeQuota: 100000}
	recipient := identityschema.User{Id: 9948, ExternalId: "LIFE02", Username: "lifetime-recipient", AffCode: "LIFE02", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(&sender).Error)
	require.NoError(t, db.Create(&recipient).Error)
	require.NoError(t, ConfigureWalletTransferPassword(sender.Id, "", "Paypass123"))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return billingapp.CreditClaudeWalletQuotaTx(tx, recipient.Id, math.MaxInt64, "life:fund", "redemption_credit")
	}))
	var account billingschema.BillingAccount
	require.NoError(t, db.Where("owner_id = ? AND account_type = ?", recipient.Id, "claude_wallet").First(&account).Error)
	reserved, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, RequestID: "life", ReservedAmount: math.MaxInt64, IdempotencyKey: "life:reserve"})
	require.NoError(t, err)
	_, err = billingdomain.SettleReservation(billingdomain.SettleReservationParams{ReservationID: reserved.ReservationID, ActualAmount: math.MaxInt64, IdempotencyKey: "life:settle"})
	require.NoError(t, err)
	require.NoError(t, db.Model(&recipient).Update("claude_quota", 0).Error)
	_, err = TransferWalletQuota(context.Background(), sender.Id, CreateWalletTransferRequest{RecipientExternalId: recipient.ExternalId, AmountQuota: minimumWalletTransferQuota(), PaymentPassword: "Paypass123", RequestId: "life:transfer"})
	require.ErrorContains(t, err, "credit exceeds integer limit")
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.Where("account_id = ?", account.AccountID).First(&snapshot).Error)
	require.Zero(t, snapshot.AvailableBalance)
	require.EqualValues(t, math.MaxInt64, snapshot.GrantedTotal)
	require.EqualValues(t, math.MaxInt64, snapshot.ConsumedTotal)
	var savedSender identityschema.User
	require.NoError(t, db.First(&savedSender, sender.Id).Error)
	require.Equal(t, sender.ClaudeQuota, savedSender.ClaudeQuota)
	var transfers, senderAccounts int64
	require.NoError(t, db.Model(&commerceschema.WalletTransfer{}).Count(&transfers).Error)
	require.NoError(t, db.Model(&billingschema.BillingAccount{}).Where("owner_id = ?", sender.Id).Count(&senderAccounts).Error)
	require.Zero(t, transfers)
	require.Zero(t, senderAccounts, "recipient overflow must roll back sender bootstrap, debit, fee, and source allocation")
}
