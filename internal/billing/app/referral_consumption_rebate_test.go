package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func referralTestDB(t *testing.T) (*gorm.DB, int64) {
	t.Helper()
	original, sqliteFlag, pgFlag := platformdb.DB, platformdb.UsingSQLite, platformdb.UsingPostgreSQL
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	platformdb.DB, platformdb.UsingSQLite, platformdb.UsingPostgreSQL = db, true, false
	t.Cleanup(func() {
		platformdb.DB, platformdb.UsingSQLite, platformdb.UsingPostgreSQL = original, sqliteFlag, pgFlag
		_ = sqlDB.Close()
	})
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &commerceschema.TopUp{}, &commerceschema.SubscriptionOrder{}, &commerceschema.UserSubscription{},
		&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{},
		&billingschema.FundingLot{}, &billingschema.FundingAllocation{}, &billingschema.FundingSourcePolicy{}, &billingschema.ReferralRebateProgram{}, &billingschema.ReferralConsumptionRebate{}, &marketplaceschema.Settlement{}))
	if !db.Migrator().HasColumn(&commerceschema.TopUp{}, "paid_quota") {
		require.NoError(t, db.Exec("ALTER TABLE top_ups ADD COLUMN paid_quota bigint NOT NULL DEFAULT 0").Error)
	}
	now := time.Now().UTC().Unix()
	require.NoError(t, db.Create(&billingschema.ReferralRebateProgram{ID: 1, RateBPS: 300, EffectiveAt: now - 3600}).Error)
	require.NoError(t, db.Create(&[]identityschema.User{
		{Id: 1, Username: "inviter", Password: "password", AffCode: "inviter", CreatedAt: now - 10000},
		{Id: 2, Username: "invitee", Password: "password", AffCode: "invitee", CreatedAt: now - 1000, InviterId: 1},
	}).Error)
	return db, now
}

func referralPaidWallet(t *testing.T, db *gorm.DB, amount, paid int64) string {
	t.Helper()
	require.NoError(t, db.Create(&commerceschema.TopUp{UserId: 2, Amount: amount, Money: float64(paid), TradeNo: "paid", Status: "success"}).Error)
	require.NoError(t, db.Model(&commerceschema.TopUp{}).Where("trade_no = ?", "paid").Update("paid_quota", paid).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := CreditClaudeWalletQuotaTx(tx, 2, int(amount), "topup:paid:unified", "topup_credit"); err != nil {
			return err
		}
		if paid > 0 {
			return SnapshotTopupPaidQuotaTx(tx, "paid", paid)
		}
		return nil
	}))
	var account billingschema.BillingAccount
	require.NoError(t, db.First(&account, "owner_type = ? AND owner_id = ? AND account_type = ?", "user", 2, "claude_wallet").Error)
	return account.AccountID
}

func referralSettle(t *testing.T, db *gorm.DB, accountID, requestID, key string, reserved, actual int64, at int64) billingschema.BillingSettlement {
	t.Helper()
	reservation, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: accountID, RequestID: requestID, ReservedAmount: reserved, IdempotencyKey: key + ":reserve"})
	require.NoError(t, err)
	settleKey := key + ":settle"
	if len(key) > 13 && key[:13] == "subscription:" {
		settleKey += ":" + reservation.ReservationID
	}
	settlement, err := billingdomain.SettleReservation(billingdomain.SettleReservationParams{ReservationID: reservation.ReservationID, UsageEvidenceID: requestID, ActualAmount: actual, IdempotencyKey: settleKey})
	require.NoError(t, err)
	if at > 0 {
		require.NoError(t, db.Model(&settlement).Update("settled_at", time.Unix(at, 0).UTC()).Error)
	}
	return *settlement
}

func TestReferralRebateUsesPaidFIFOAndNetSettledUsageWithoutRecursion(t *testing.T) {
	db, now := referralTestDB(t)
	require.NoError(t, db.Create(&[]billingschema.FundingSourcePolicy{{Source: "topup", RevenueMultiplier: 99}, {Source: "other", RevenueMultiplier: 99}}).Error)
	accountID := referralPaidWallet(t, db, 1000, 800)
	require.NoError(t, db.Model(&billingschema.FundingLot{}).Where("idempotency_key = ?", "topup:paid:unified").Update("created_at", time.Unix(now-100, 0)).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return CreditClaudeWalletQuotaTx(tx, 2, 500, "gift", "admin_gift") }))
	settlement := referralSettle(t, db, accountID, "api-1", "relay:wallet:api-1", 1500, 1250, now)
	processed, err := processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	require.Positive(t, processed)
	var receipt billingschema.ReferralConsumptionRebate
	require.NoError(t, db.First(&receipt, "settlement_id = ?", settlement.SettlementID).Error)
	require.EqualValues(t, 800, receipt.EligibleQuota)
	require.EqualValues(t, 24, receipt.RebateQuota)
	var inviter identityschema.User
	require.NoError(t, db.First(&inviter, 1).Error)
	require.Equal(t, 24, inviter.ClaudeQuota)
	var lot billingschema.FundingLot
	require.NoError(t, db.First(&lot, "idempotency_key = ?", "referral-consumption:"+settlement.SettlementID).Error)
	require.Equal(t, "other", lot.Source)
	require.Zero(t, lot.PaidQuota)
	require.Zero(t, lot.RevenueMultiplier)
	var allocated int64
	require.NoError(t, db.Model(&billingschema.FundingAllocation{}).Where("request_id = ?", "api-1").Select("SUM(amount)").Scan(&allocated).Error)
	require.EqualValues(t, 1250, allocated) // Includes settled use; not reserved 1500.
	// Force a retry of the durable event: neither receipt nor wallet can double.
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Where("aggregate_id = ?", settlement.SettlementID).Update("status", "pending").Error)
	_, err = processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, 1).Error)
	require.Equal(t, 24, inviter.ClaudeQuota)
	// Spending referral earnings cannot create another upstream referral reward.
	require.NoError(t, db.Create(&identityschema.User{Id: 3, Username: "upstream", Password: "password", AffCode: "upstream", CreatedAt: now - 1000}).Error)
	require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", 1).Updates(map[string]any{"inviter_id": 3, "created_at": now - 100}).Error)
	referralSettle(t, db, lot.AccountID, "rebate-use", "relay:wallet:rebate-use", 24, 24, now)
	_, err = processLedgerOutboxAccount(context.Background(), lot.AccountID)
	require.NoError(t, err)
	var recursive billingschema.ReferralConsumptionRebate
	require.NoError(t, db.First(&recursive, "invitee_id = ?", 1).Error)
	require.Zero(t, recursive.RebateQuota)
	require.Equal(t, "unpaid_funding", recursive.ExclusionReason)
}

func TestReferralRebatePreservesSmallPaidFractions(t *testing.T) {
	db, now := referralTestDB(t)
	accountID := referralPaidWallet(t, db, 110, 100)
	for index := 0; index < 11; index++ {
		request := fmt.Sprintf("fraction-%d", index)
		referralSettle(t, db, accountID, request, "relay:wallet:"+request, 10, 10, now)
	}
	_, err := processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	var sum struct{ Eligible, Rebate int64 }
	require.NoError(t, db.Model(&billingschema.ReferralConsumptionRebate{}).Select("SUM(eligible_quota) AS eligible, SUM(rebate_quota) AS rebate").Scan(&sum).Error)
	require.EqualValues(t, 100, sum.Eligible)
	require.EqualValues(t, 3, sum.Rebate)
}

func TestReferralRebateCreditPackPreservesSmallPaidFractions(t *testing.T) {
	db, now := referralTestDB(t)
	sub := commerceschema.UserSubscription{UserId: 2, AmountTotal: 110, CreditPackGrantedQuota: 110, CreditPackPaidQuota: 100}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{UserId: 2, TradeNo: "small-pack", Money: 100, Status: "success", TargetSubscriptionId: sub.Id, CreditPackQuota: 110, CreditPackPaidQuota: 100}).Error)
	account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id)})
	require.NoError(t, err)
	_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 110, IdempotencyKey: "small-pack-credit"})
	require.NoError(t, err)
	for index := 0; index < 11; index++ {
		request := fmt.Sprintf("pack-fraction-%d", index)
		referralSettle(t, db, account.AccountID, request, "subscription:"+request, 10, 10, now)
		require.NoError(t, attachCreditPackReferralContext(referralPackRelayInfo(sub, account.AccountID, request, request, false), 10))
	}
	_, err = processLedgerOutboxAccount(context.Background(), account.AccountID)
	require.NoError(t, err)
	var sum struct{ Eligible, Rebate int64 }
	require.NoError(t, db.Model(&billingschema.ReferralConsumptionRebate{}).Select("SUM(eligible_quota) AS eligible, SUM(rebate_quota) AS rebate").Scan(&sum).Error)
	require.EqualValues(t, 100, sum.Eligible)
	require.EqualValues(t, 3, sum.Rebate)
}

func TestReferralRebateMissingProgramRetainsSettlementForRetry(t *testing.T) {
	db, now := referralTestDB(t)
	accountID := referralPaidWallet(t, db, 1000, 1000)
	settlement := referralSettle(t, db, accountID, "program", "relay:wallet:program", 100, 100, now)
	require.NoError(t, db.Delete(&billingschema.ReferralRebateProgram{}, 1).Error)
	_, err := RunLedgerWorkerBatch(context.Background(), 100)
	require.NoError(t, err)
	var event billingschema.BillingOutboxEvent
	require.NoError(t, db.First(&event, "aggregate_id = ?", settlement.SettlementID).Error)
	require.Equal(t, "pending", event.Status)
	require.Contains(t, event.LastError, "load referral rebate program")
	require.NoError(t, db.Create(&billingschema.ReferralRebateProgram{ID: 1, RateBPS: 300, EffectiveAt: now - 3600}).Error)
	_, err = RunLedgerWorkerBatch(context.Background(), 100)
	require.NoError(t, err)
	var receipt billingschema.ReferralConsumptionRebate
	require.NoError(t, db.First(&receipt, "settlement_id = ?", settlement.SettlementID).Error)
	require.EqualValues(t, 3, receipt.RebateQuota)
}

func TestReferralWorkerPublishesOnlySelectedEventIDs(t *testing.T) {
	db, now := referralTestDB(t)
	accountID := referralPaidWallet(t, db, 1000, 1000)
	referralSettle(t, db, accountID, "publish", "relay:wallet:publish", 100, 100, now)
	inserted := false
	const callback = "inject-new-pending-event"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]billingschema.BillingOutboxEvent); ok && !inserted {
			inserted = true
			tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Create(&billingschema.BillingOutboxEvent{AccountID: accountID, EventType: "billing.account_credited", IdempotencyKey: "late-event"}).Error)
		}
	}))
	_, err := processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	require.NoError(t, db.Callback().Query().Remove(callback))
	require.True(t, inserted)
	var late billingschema.BillingOutboxEvent
	require.NoError(t, db.First(&late, "idempotency_key = ?", "late-event").Error)
	require.Equal(t, "pending", late.Status)
}

func TestReferralRebateAccumulatesRoundingAcrossSettlements(t *testing.T) {
	db, now := referralTestDB(t)
	accountID := referralPaidWallet(t, db, 1000, 1000)
	for index, amount := range []int64{33, 33, 34} {
		request := fmt.Sprintf("small-%d", index)
		referralSettle(t, db, accountID, request, "relay:wallet:"+request, amount, amount, now)
	}
	_, err := processLedgerOutboxAccount(context.Background(), accountID)
	require.NoError(t, err)
	var sum struct{ Eligible, Rebate int64 }
	require.NoError(t, db.Model(&billingschema.ReferralConsumptionRebate{}).Select("SUM(eligible_quota) AS eligible, SUM(rebate_quota) AS rebate").Scan(&sum).Error)
	require.EqualValues(t, 100, sum.Eligible)
	require.EqualValues(t, 3, sum.Rebate)
}

func TestReferralRebateTimeWindowAndNonConsumptionExclusions(t *testing.T) {
	for _, test := range []struct {
		name      string
		offset    int64
		effective int64
		actual    int64
		key       string
		expected  bool
	}{
		{name: "registration inclusive", offset: 0, effective: -1000, actual: 100, key: "relay:wallet:time", expected: true},
		{name: "last second", offset: 604799, effective: -1000, actual: 100, key: "relay:wallet:time", expected: true},
		{name: "seven days exclusive", offset: 604800, effective: -1000, actual: 100, key: "relay:wallet:time"},
		{name: "before registration", offset: -1, effective: -1000, actual: 100, key: "relay:wallet:time"},
		{name: "before rollout", offset: 0, effective: 1, actual: 100, key: "relay:wallet:time"},
		{name: "zero actual", offset: 0, effective: -1000, actual: 0, key: "relay:wallet:time"},
		{name: "purchase transfer", offset: 0, effective: -1000, actual: 100, key: "blind-box-purchase:time"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, now := referralTestDB(t)
			require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", 2).Update("created_at", now).Error)
			require.NoError(t, db.Model(&billingschema.ReferralRebateProgram{}).Where("id = ?", 1).Update("effective_at", now+test.effective).Error)
			accountID := referralPaidWallet(t, db, 1000, 1000)
			referralSettle(t, db, accountID, "time", test.key, 100, test.actual, now+test.offset)
			_, err := processLedgerOutboxAccount(context.Background(), accountID)
			require.NoError(t, err)
			var count int64
			require.NoError(t, db.Model(&billingschema.ReferralConsumptionRebate{}).Count(&count).Error)
			if test.expected {
				require.EqualValues(t, 1, count)
			} else {
				require.Zero(t, count)
			}
		})
	}
}

func TestReferralRebateCreditPackExcludesBonusAndUnprovenConversions(t *testing.T) {
	for _, test := range []struct {
		name          string
		granted, paid int64
		paidOrder     bool
		expected      int64
		reason        string
	}{
		{name: "paid permanent package", granted: 1100, paid: 1000, paidOrder: true, expected: 1000},
		{name: "legacy tenfold balance", reason: "legacy_paid_snapshot_missing"},
		{name: "converted unproven gift", granted: 1100, paid: 1100, reason: "conversion_paid_snapshot_missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, now := referralTestDB(t)
			sub := commerceschema.UserSubscription{UserId: 2, AmountTotal: 1100, CreditPackGrantedQuota: test.granted, CreditPackPaidQuota: test.paid}
			require.NoError(t, db.Create(&sub).Error)
			if test.paidOrder {
				require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{UserId: 2, TradeNo: "pack", Money: 1000, Status: "success", TargetSubscriptionId: sub.Id, CreditPackQuota: test.granted, CreditPackPaidQuota: test.paid}).Error)
			}
			account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "subscription", OwnerType: "user_subscription", OwnerID: int64(sub.Id)})
			require.NoError(t, err)
			_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 1100, IdempotencyKey: "pack-credit"})
			require.NoError(t, err)
			settlement := referralSettle(t, db, account.AccountID, "pack-api", "subscription:pack-api", 1100, 1100, now)
			if sub.CreditPackGrantedQuota > 0 {
				require.NoError(t, attachCreditPackReferralContext(referralPackRelayInfo(sub, account.AccountID, "pack-api", "pack-api", false), 1100))
			}
			_, err = processLedgerOutboxAccount(context.Background(), account.AccountID)
			require.NoError(t, err)
			var receipt billingschema.ReferralConsumptionRebate
			require.NoError(t, db.First(&receipt, "settlement_id = ?", settlement.SettlementID).Error)
			require.Equal(t, test.expected, receipt.EligibleQuota)
			require.Equal(t, test.expected*3/100, receipt.RebateQuota)
			require.Equal(t, test.reason, receipt.ExclusionReason)
		})
	}
}

func TestReferralRebateFailureRollsBackReceiptAllocationAndPublishThenRetries(t *testing.T) {
	db, now := referralTestDB(t)
	accountID := referralPaidWallet(t, db, 1000, 1000)
	settlement := referralSettle(t, db, accountID, "retry", "relay:wallet:retry", 100, 100, now)
	const callback = "fail-referral-credit"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if entry, ok := tx.Statement.Dest.(*billingschema.BillingLedgerEntry); ok && entry.ReasonCode == "referral_consumption_rebate" {
			tx.AddError(errors.New("injected referral credit failure"))
		}
	}))
	_, err := RunLedgerWorkerBatch(context.Background(), 100)
	require.NoError(t, err) // Durable outbox records failure and retains the event.
	var event billingschema.BillingOutboxEvent
	require.NoError(t, db.First(&event, "aggregate_id = ?", settlement.SettlementID).Error)
	require.Equal(t, "pending", event.Status)
	require.Positive(t, event.Attempts)
	require.Contains(t, event.LastError, "injected referral credit failure")
	for _, model := range []any{&billingschema.ReferralConsumptionRebate{}, &billingschema.FundingAllocation{}} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		require.Zero(t, count)
	}
	require.NoError(t, db.Callback().Create().Remove(callback))
	_, err = RunLedgerWorkerBatch(context.Background(), 100)
	require.NoError(t, err)
	var receipt billingschema.ReferralConsumptionRebate
	require.NoError(t, db.First(&receipt, "settlement_id = ?", settlement.SettlementID).Error)
	require.EqualValues(t, 3, receipt.RebateQuota)
}
