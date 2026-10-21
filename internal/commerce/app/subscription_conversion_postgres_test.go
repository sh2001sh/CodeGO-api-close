package app

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func TestWalletConversionAndResetRedemptionPostgres(t *testing.T) {
	dsn := os.Getenv("WALLET_CONVERSION_TEST_DSN")
	if dsn == "" {
		t.Skip("WALLET_CONVERSION_TEST_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var database string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&database).Error)
	require.True(t, strings.HasPrefix(database, "codego_wallet_conversion_"), "isolated wallet conversion database required")
	oldDB, oldLog := platformdb.DB, platformdb.LogDB
	oldPG, oldSQLite, oldMySQL, oldRedis := platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled
	platformdb.DB, platformdb.LogDB = db, db
	platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = true, false, false, false
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(24)
	t.Cleanup(func() {
		_ = sqlDB.Close()
		platformdb.DB, platformdb.LogDB = oldDB, oldLog
		platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = oldPG, oldSQLite, oldMySQL, oldRedis
	})
	restoreSubscriptionClaudeConversionConfigForTest(t)
	commerceschema.SubscriptionClaudeConversionEnabled = true
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &commerceschema.SubscriptionPlan{}, &commerceschema.UserSubscription{}, &commerceschema.SubscriptionOrder{}, &commerceschema.SubscriptionClaudeConversion{}, &commerceschema.SubscriptionResetOpportunityAccount{}, &commerceschema.SubscriptionResetOpportunityLedger{}, &commerceschema.GroupBuyOrder{}, &commerceschema.GroupBuyMember{}, &commerceschema.TopUp{}, &billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}, &billingschema.FundingLot{}, &billingschema.FundingAllocation{}))
	seed := func(t *testing.T) (identityschema.User, commerceschema.SubscriptionPlan, commerceschema.UserSubscription) {
		t.Helper()
		stamp := fmt.Sprint(time.Now().UnixNano())
		user := identityschema.User{Username: "wallet-pg-" + stamp, AffCode: "wp" + stamp, Status: constant.UserStatusEnabled, ClaudeQuota: 100, Group: "default"}
		require.NoError(t, db.Create(&user).Error)
		plan := commerceschema.SubscriptionPlan{Title: "Legacy PG", PriceAmount: 299, PlanType: commerceschema.SubscriptionPlanTypeMonthly, TotalAmount: int64(platformruntime.QuotaPerUnit) * 100, QuotaResetPeriod: commerceschema.SubscriptionResetNever}
		require.NoError(t, db.Create(&plan).Error)
		sub := commerceschema.UserSubscription{UserId: user.Id, PlanId: plan.Id, AmountTotal: plan.TotalAmount, Status: "active", StartTime: time.Now().Add(-time.Hour).Unix(), EndTime: time.Now().Add(time.Hour).Unix()}
		require.NoError(t, db.Create(&sub).Error)
		return user, plan, sub
	}
	runConcurrent := func(fn func(int) error) []error {
		const count = 12
		start := make(chan struct{})
		results := make([]error, count)
		var workers sync.WaitGroup
		for i := range count {
			workers.Go(func() { <-start; results[i] = fn(i) })
		}
		close(start)
		workers.Wait()
		return results
	}

	for _, sameKey := range []bool{true, false} {
		t.Run(fmt.Sprintf("credit pack concurrent full conversion same key %t", sameKey), func(t *testing.T) {
			user, plan, _ := seed(t)
			sub := seedCreditPackConversion(t, db, user.Id, plan.Id)
			results := runConcurrent(func(i int) error {
				if sameKey {
					i = 0
				}
				_, err := ConvertMonthlyPassToUnifiedCredit(fmt.Sprintf("pg-pack-%d-%d", sub.Id, i), user.Id, sub.Id, 100)
				return err
			})
			success := 0
			for _, err := range results {
				if err == nil {
					success++
				} else {
					require.ErrorIs(t, err, commerceschema.ErrSubscriptionClaudeConversionNoTarget)
				}
			}
			if sameKey {
				require.Equal(t, 12, success)
			} else {
				require.Equal(t, 1, success)
			}
			var saved identityschema.User
			require.NoError(t, db.First(&saved, user.Id).Error)
			require.Equal(t, 32000100, saved.ClaudeQuota)
			var count int64
			require.NoError(t, db.Model(&commerceschema.SubscriptionClaudeConversion{}).Where("user_subscription_id = ?", sub.Id).Count(&count).Error)
			require.EqualValues(t, 1, count)
		})
	}
	t.Run("credit pack concurrent partial conversions use current remainder", func(t *testing.T) {
		user, plan, _ := seed(t)
		sub := seedCreditPackConversion(t, db, user.Id, plan.Id)
		for _, err := range runConcurrent(func(i int) error {
			_, err := ConvertMonthlyPassToUnifiedCredit(fmt.Sprintf("pg-pack-partial-%d-%d", sub.Id, i), user.Id, sub.Id, 25)
			return err
		}) {
			require.NoError(t, err)
		}
		var records []commerceschema.SubscriptionClaudeConversion
		require.NoError(t, db.Where("user_subscription_id = ?", sub.Id).Find(&records).Error)
		require.Len(t, records, 12)
		remaining, credited := sub.AmountTotal, int64(0)
		for range 12 {
			remaining -= remaining * 25 / 100
		}
		for _, record := range records {
			credited += int64(record.TargetQuota)
		}
		var source billingschema.BillingBalanceSnapshot
		require.NoError(t, db.Table("billing.balance_snapshots AS b").Select("b.*").Joins("JOIN billing.accounts a ON a.account_id=b.account_id").Where("a.owner_type = ? AND a.owner_id = ?", "user_subscription", sub.Id).First(&source).Error)
		require.Equal(t, remaining, source.AvailableBalance)
		require.Zero(t, source.ReservedBalance)
		require.LessOrEqual(t, credited, (sub.AmountTotal-remaining)*32000000/33600000)
	})
	t.Run("same conversion key twelve callers one credit", func(t *testing.T) {
		user, _, sub := seed(t)
		key := fmt.Sprintf("pg-convert-same-%d", user.Id)
		results := runConcurrent(func(_ int) error { _, err := ConvertMonthlyPassToUnifiedCredit(key, user.Id, sub.Id, 100); return err })
		for _, err := range results {
			require.NoError(t, err)
		}
		var saved identityschema.User
		require.NoError(t, db.First(&saved, user.Id).Error)
		require.EqualValues(t, 100+quotaUnitsFromUSD(299), saved.ClaudeQuota)
		var count int64
		require.NoError(t, db.Model(&commerceschema.SubscriptionClaudeConversion{}).Where("user_id = ?", user.Id).Count(&count).Error)
		require.EqualValues(t, 1, count)
	})
	t.Run("renewed legacy remainder twelve callers one wallet credit", func(t *testing.T) {
		user, plan, sub := seed(t)
		sub.AmountUsed = sub.AmountTotal / 2
		require.NoError(t, db.Save(&sub).Error)
		for i, paid := range []float64{199, 100} {
			require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{
				UserId: user.Id, PlanId: plan.Id, TargetSubscriptionId: sub.Id,
				TradeNo: fmt.Sprintf("pg-renewed-%d-%d", sub.Id, i), Money: paid,
				Status: constant.TopUpStatusSuccess,
			}).Error)
		}
		preview := BuildSubscriptionClaudeConversionPreview(&plan, &sub)
		require.True(t, preview.Eligible)
		require.EqualValues(t, quotaUnitsFromUSD(149.5), preview.BaseQuota)
		key := fmt.Sprintf("pg-renewed-convert-%d", sub.Id)
		for _, err := range runConcurrent(func(_ int) error {
			_, err := ConvertMonthlyPassToUnifiedCredit(key, user.Id, sub.Id, 50)
			return err
		}) {
			require.NoError(t, err)
		}
		var saved identityschema.User
		require.NoError(t, db.First(&saved, user.Id).Error)
		require.EqualValues(t, 100+quotaUnitsFromUSD(149.5), saved.ClaudeQuota)
		var count int64
		require.NoError(t, db.Model(&commerceschema.SubscriptionClaudeConversion{}).Where("user_id = ?", user.Id).Count(&count).Error)
		require.EqualValues(t, 1, count)
		_, err = ConvertMonthlyPassToUnifiedCredit(key+"-different", user.Id, sub.Id, 50)
		require.Error(t, err)
	})
	t.Run("distinct conversion keys cannot double spend", func(t *testing.T) {
		user, _, sub := seed(t)
		results := runConcurrent(func(i int) error {
			_, err := ConvertMonthlyPassToUnifiedCredit(fmt.Sprintf("pg-distinct-%d-%d", user.Id, i), user.Id, sub.Id, 100)
			return err
		})
		count := 0
		for _, err := range results {
			if err == nil {
				count++
			} else {
				require.ErrorIs(t, err, commerceschema.ErrSubscriptionClaudeConversionNoTarget)
			}
		}
		require.Equal(t, 1, count)
	})
	t.Run("paid group three after two refunds and maximum request key", func(t *testing.T) {
		user, plan, sub := seed(t)
		room := commerceschema.GroupBuyOrder{PlanId: plan.Id, InitiatorId: user.Id, Status: commerceschema.GroupBuyStatusPending, CurrentCount: 99}
		require.NoError(t, db.Create(&room).Error)
		for i := range 5 {
			memberUser, memberSub := user, sub
			if i > 0 {
				memberUser = identityschema.User{Username: fmt.Sprintf("pg-group-%d-%d", user.Id, i), AffCode: fmt.Sprintf("pg-%d-%d", user.Id, i), Group: "default"}
				require.NoError(t, db.Create(&memberUser).Error)
				memberSub = sub
				memberSub.Id = 0
				memberSub.UserId = memberUser.Id
				require.NoError(t, db.Create(&memberSub).Error)
			}
			order := commerceschema.SubscriptionOrder{UserId: memberUser.Id, PlanId: plan.Id, TargetSubscriptionId: memberSub.Id, GroupBuyId: room.Id, TradeNo: fmt.Sprintf("pg-paid-group-%d-%d", user.Id, i), Money: 299, Status: constant.TopUpStatusSuccess}
			if i >= 3 {
				order.RefundStatus = commerceschema.RefundStatusSuccess
				order.RefundAmount = 299
			}
			require.NoError(t, db.Create(&order).Error)
			require.NoError(t, db.Create(&commerceschema.GroupBuyMember{GroupBuyId: room.Id, UserId: memberUser.Id, UserSubscriptionId: memberSub.Id, OrderId: order.Id}).Error)
		}
		key := fmt.Sprintf("%0128d", user.Id)
		result, err := ConvertMonthlyPassToUnifiedCredit(key, user.Id, sub.Id, 100)
		require.NoError(t, err)
		require.Equal(t, 3, result.BonusPercent)
		require.Equal(t, 3, result.GroupBuyMemberCount)
		require.EqualValues(t, quotaUnitsFromUSD(299), result.BaseQuota)
		require.EqualValues(t, quotaUnitsFromUSD(8.97), result.BonusQuota)
		replay, err := ConvertMonthlyPassToUnifiedCredit(key, user.Id, sub.Id, 100)
		require.NoError(t, err)
		require.Equal(t, result.BonusQuota, replay.BonusQuota)
		var lots []billingschema.FundingLot
		require.NoError(t, db.Where("idempotency_key IN ?", []string{monthlyPassWalletReceiptKey(key, "base"), monthlyPassWalletReceiptKey(key, "gift")}).Find(&lots).Error)
		require.Len(t, lots, 2)
		for _, lot := range lots {
			require.Zero(t, lot.PaidQuota)
			require.Zero(t, lot.RevenueMultiplier)
		}
	})
	for _, sameKey := range []bool{false, true} {
		t.Run(fmt.Sprintf("redemption concurrent same key %t", sameKey), func(t *testing.T) {
			user, _, _ := seed(t)
			stamp := fmt.Sprint(time.Now().UnixNano())
			plan := defaultSubscriptionPlans()[0]
			plan.Id = 0
			plan.Title = "PG Pack " + stamp
			require.NoError(t, db.Create(&plan).Error)
			require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityAccount{UserId: user.Id, EarnedTotal: 2, AvailableTotal: 2, LastUsedMonth: "2020-01"}).Error)
			results := runConcurrent(func(i int) error {
				if sameKey {
					i = 0
				}
				_, err := RedeemSubscriptionResetOpportunity(user.Id, RedeemSubscriptionResetOpportunityRequest{RequestID: fmt.Sprintf("pg-redeem-%d-%d", user.Id, i)})
				return err
			})
			success := 0
			for _, err := range results {
				if err == nil {
					success++
				} else {
					require.ErrorIs(t, err, commerceschema.ErrSubscriptionResetOpportunityUnavailable)
				}
			}
			expected := 2
			if sameKey {
				expected = 1
				require.Equal(t, 12, success)
			} else {
				require.Equal(t, 2, success)
			}
			var count int64
			require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("user_id = ? AND source = ?", user.Id, "reset_redemption").Count(&count).Error)
			require.EqualValues(t, expected, count)
			var account commerceschema.SubscriptionResetOpportunityAccount
			require.NoError(t, db.First(&account, "user_id = ?", user.Id).Error)
			require.Equal(t, 2-expected, account.AvailableTotal)
			require.Equal(t, "2020-01", account.LastUsedMonth)
		})
	}
	t.Run("SQL failure rolls back conversion and redemption", func(t *testing.T) {
		user, _, sub := seed(t)
		require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityAccount{UserId: user.Id, EarnedTotal: 1, AvailableTotal: 1}).Error)
		const callback = "test:wallet-pg-sql-failure"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			switch tx.Statement.Dest.(type) {
			case *commerceschema.SubscriptionClaudeConversion, *commerceschema.SubscriptionResetOpportunityLedger:
				tx.AddError(tx.Exec("SELECT 1 / 0").Error)
			}
		}))
		t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
		_, err := ConvertMonthlyPassToUnifiedCredit(fmt.Sprintf("pg-rollback-%d", user.Id), user.Id, sub.Id, 100)
		require.ErrorContains(t, err, "division by zero")
		_, err = RedeemSubscriptionResetOpportunity(user.Id, RedeemSubscriptionResetOpportunityRequest{RequestID: "pg-rollback"})
		require.ErrorContains(t, err, "division by zero")
		var saved identityschema.User
		require.NoError(t, db.First(&saved, user.Id).Error)
		require.Equal(t, 100, saved.ClaudeQuota)
		var savedSub commerceschema.UserSubscription
		require.NoError(t, db.First(&savedSub, sub.Id).Error)
		require.Zero(t, savedSub.AmountUsed)
		require.Equal(t, "active", savedSub.Status)
		var account commerceschema.SubscriptionResetOpportunityAccount
		require.NoError(t, db.First(&account, "user_id = ?", user.Id).Error)
		require.Equal(t, 1, account.AvailableTotal)
		var count int64
		require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("user_id = ? AND source = ?", user.Id, "reset_redemption").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, db.Model(&billingschema.BillingAccount{}).Where("(owner_type = ? AND owner_id = ?) OR (owner_type = ? AND owner_id IN (?))", "user", user.Id, "user_subscription", db.Model(&commerceschema.UserSubscription{}).Select("id").Where("user_id = ?", user.Id)).Count(&count).Error)
		require.Zero(t, count)
	})
	t.Run("refund order lock does not invert conversion locks", func(t *testing.T) {
		user, plan, sub := seed(t)
		room := commerceschema.GroupBuyOrder{PlanId: plan.Id, InitiatorId: user.Id, Status: commerceschema.GroupBuyStatusCompleted}
		require.NoError(t, db.Create(&room).Error)
		order := commerceschema.SubscriptionOrder{UserId: user.Id, PlanId: plan.Id, TargetSubscriptionId: sub.Id, GroupBuyId: room.Id, TradeNo: fmt.Sprintf("pg-refund-lock-%d", user.Id), Money: 299, Status: constant.TopUpStatusSuccess}
		require.NoError(t, db.Create(&order).Error)
		require.NoError(t, db.Create(&commerceschema.GroupBuyMember{GroupBuyId: room.Id, UserId: user.Id, UserSubscriptionId: sub.Id, OrderId: order.Id}).Error)
		refundTx := db.Begin()
		require.NoError(t, refundTx.Error)
		defer refundTx.Rollback()
		require.NoError(t, refundTx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, order.Id).Error)
		done := make(chan error, 1)
		go func() {
			_, err := ConvertMonthlyPassToUnifiedCredit(fmt.Sprintf("pg-no-deadlock-%d", user.Id), user.Id, sub.Id, 100)
			done <- err
		}()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			_ = refundTx.Rollback()
			t.Fatal("conversion blocked on refund-owned order row")
		}
		var saved commerceschema.UserSubscription
		require.NoError(t, refundTx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&saved, sub.Id).Error)
		require.Equal(t, "cancelled", saved.Status)
	})
	t.Run("committed gateway reservation with delayed projection remains held", func(t *testing.T) {
		user, _, sub := seed(t)
		account, err := billingdomain.EnsureBillingAccount(billingdomain.EnsureAccountParams{AccountType: "claude_wallet", OwnerType: "user", OwnerID: int64(user.Id), QuotaUnit: "quota"})
		require.NoError(t, err)
		_, err = billingdomain.CreditAccount(billingdomain.CreditAccountParams{AccountID: account.AccountID, Amount: 100, IdempotencyKey: fmt.Sprintf("pg-bootstrap-%d", user.Id), ReasonCode: "mirror_bootstrap"})
		require.NoError(t, err)
		// The first gateway transaction committed its hold, while its second
		// legacy-projection update has yet to run.
		reservation, err := billingdomain.CreateReservation(billingdomain.CreateReservationParams{AccountID: account.AccountID, RequestID: fmt.Sprintf("pg-gap-%d", user.Id), ReservedAmount: 20, IdempotencyKey: fmt.Sprintf("pg-gap-%d", user.Id)})
		require.NoError(t, err)
		result, err := ConvertMonthlyPassToUnifiedCredit(fmt.Sprintf("pg-reserved-%d", user.Id), user.Id, sub.Id, 100)
		require.NoError(t, err)
		require.EqualValues(t, 80+quotaUnitsFromUSD(299), result.QuotaAfter)
		replay, err := ConvertMonthlyPassToUnifiedCredit(fmt.Sprintf("pg-reserved-%d", user.Id), user.Id, sub.Id, 100)
		require.NoError(t, err)
		require.Equal(t, result.QuotaAfter, replay.QuotaAfter, "replay also reads canonical balance while projection is delayed")
		var snapshot billingschema.BillingBalanceSnapshot
		require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
		require.EqualValues(t, 80+quotaUnitsFromUSD(299), snapshot.AvailableBalance)
		require.EqualValues(t, 20, snapshot.ReservedBalance)
		var saved identityschema.User
		require.NoError(t, db.First(&saved, user.Id).Error)
		require.EqualValues(t, 100+quotaUnitsFromUSD(299), saved.ClaudeQuota, "conversion only increments the projection; gateway still owns its pending decrement")
		require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", user.Id).Update("claude_quota", gorm.Expr("claude_quota - ?", 20)).Error)
		require.NoError(t, db.First(&saved, user.Id).Error)
		require.EqualValues(t, snapshot.AvailableBalance, saved.ClaudeQuota)
		_, err = billingdomain.SettleReservation(billingdomain.SettleReservationParams{ReservationID: reservation.ReservationID, ActualAmount: 20, IdempotencyKey: fmt.Sprintf("pg-gap-settle-%d", user.Id)})
		require.NoError(t, err)
		var count int64
		require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("account_id = ? AND reason_code = ?", account.AccountID, "ledger_sync_reconcile").Count(&count).Error)
		require.Zero(t, count, "a delayed projection must never mint compensation for a native hold")
	})
	t.Run("twelve mixed refresh and redemption callers share one opportunity", func(t *testing.T) {
		user, _, _ := seed(t)
		pack := defaultSubscriptionPlans()[0]
		pack.Id = 0
		require.NoError(t, db.Create(&pack).Error)
		require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityAccount{UserId: user.Id, EarnedTotal: 1, AvailableTotal: 1}).Error)
		results := runConcurrent(func(i int) error {
			if i%2 == 0 {
				_, err := UseUserSubscriptionResetOpportunity(user.Id)
				return err
			}
			_, err := RedeemSubscriptionResetOpportunity(user.Id, RedeemSubscriptionResetOpportunityRequest{RequestID: fmt.Sprintf("mixed-%d", i)})
			return err
		})
		success := 0
		for _, err := range results {
			if err == nil {
				success++
			} else {
				require.ErrorIs(t, err, commerceschema.ErrSubscriptionResetOpportunityUnavailable)
			}
		}
		require.Equal(t, 1, success)
		var count int64
		require.NoError(t, db.Model(&commerceschema.SubscriptionResetOpportunityLedger{}).Where("user_id = ? AND change_type IN ?", user.Id, []string{"use", "redeem"}).Count(&count).Error)
		require.EqualValues(t, 1, count)
	})
	for _, firstRefresh := range []bool{true, false} {
		t.Run(fmt.Sprintf("one shared opportunity refresh first %t", firstRefresh), func(t *testing.T) {
			user, _, sub := seed(t)
			used := quotaUnitsFromUSD(20)
			require.NoError(t, db.Model(&sub).Update("amount_used", used).Error)
			pack := defaultSubscriptionPlans()[0]
			pack.Id = 0
			require.NoError(t, db.Create(&pack).Error)
			require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityAccount{UserId: user.Id, EarnedTotal: 1, AvailableTotal: 1}).Error)
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			const callback = "test:shared-reset-account-lock"
			wanted := "redeem"
			if firstRefresh {
				wanted = "use"
			}
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if ledger, ok := tx.Statement.Dest.(*commerceschema.SubscriptionResetOpportunityLedger); ok && ledger.UserId == user.Id && ledger.ChangeType == wanted {
					close(entered)
					<-release
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
			refresh := func() error { _, err := UseUserSubscriptionResetOpportunity(user.Id); return err }
			redeem := func() error {
				_, err := RedeemSubscriptionResetOpportunity(user.Id, RedeemSubscriptionResetOpportunityRequest{RequestID: "shared-one"})
				return err
			}
			first, second := refresh, redeem
			if !firstRefresh {
				first, second = redeem, refresh
			}
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() { firstDone <- first() }()
			select {
			case <-entered:
			case err := <-firstDone:
				t.Fatalf("first operation failed before atomic receipt: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("first operation did not hold shared account")
			}
			var visible commerceschema.SubscriptionResetOpportunityAccount
			require.NoError(t, db.First(&visible, "user_id = ?", user.Id).Error)
			require.Equal(t, 1, visible.AvailableTotal, "uncommitted opportunity consumption stays invisible")
			go func() { secondDone <- second() }()
			select {
			case err := <-secondDone:
				t.Fatalf("competing operation escaped account lock: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			unblock()
			require.NoError(t, <-firstDone)
			require.ErrorIs(t, <-secondDone, commerceschema.ErrSubscriptionResetOpportunityUnavailable)
			require.NoError(t, db.First(&visible, "user_id = ?", user.Id).Error)
			require.Zero(t, visible.AvailableTotal)
			require.Equal(t, 1, visible.UsedTotal)
			var ledgers []commerceschema.SubscriptionResetOpportunityLedger
			require.NoError(t, db.Where("user_id = ?", user.Id).Find(&ledgers).Error)
			require.Len(t, ledgers, 1)
			require.Equal(t, wanted, ledgers[0].ChangeType)
			var refreshed commerceschema.UserSubscription
			require.NoError(t, db.First(&refreshed, sub.Id).Error)
			var packs int64
			require.NoError(t, db.Model(&commerceschema.UserSubscription{}).Where("user_id = ? AND source = ?", user.Id, "reset_redemption").Count(&packs).Error)
			if firstRefresh {
				require.Zero(t, refreshed.AmountUsed)
				require.Zero(t, packs)
				require.Equal(t, currentResetOpportunityMonth(), visible.LastUsedMonth)
			} else {
				require.Equal(t, used, refreshed.AmountUsed)
				require.EqualValues(t, 1, packs)
				require.Empty(t, visible.LastUsedMonth)
			}
		})
	}
	for _, firstRefresh := range []bool{true, false} {
		t.Run(fmt.Sprintf("refresh conversion interleaving refresh first %t", firstRefresh), func(t *testing.T) {
			user, _, sub := seed(t)
			require.NoError(t, db.Model(&sub).Update("amount_used", quotaUnitsFromUSD(20)).Error)
			require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityAccount{UserId: user.Id, EarnedTotal: 1, AvailableTotal: 1}).Error)
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			const callback = "test:reset-conversion-subscription-lock"
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				pause := false
				if firstRefresh {
					if ledger, ok := tx.Statement.Dest.(*commerceschema.SubscriptionResetOpportunityLedger); ok && ledger.UserId == user.Id && ledger.ChangeType == "use" {
						pause = true
					}
				} else {
					if conversion, ok := tx.Statement.Dest.(*commerceschema.SubscriptionClaudeConversion); ok && conversion.UserId == user.Id {
						pause = true
					}
				}
				if pause {
					close(entered)
					<-release
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
			refresh := func() error { _, err := UseUserSubscriptionResetOpportunity(user.Id); return err }
			convert := func() error {
				_, err := ConvertMonthlyPassToUnifiedCredit(fmt.Sprintf("pg-refresh-race-%d", user.Id), user.Id, sub.Id, 50)
				return err
			}
			first, second := refresh, convert
			if !firstRefresh {
				first, second = convert, refresh
			}
			firstDone, secondDone := make(chan error, 1), make(chan error, 1)
			go func() { firstDone <- first() }()
			select {
			case <-entered:
			case err := <-firstDone:
				t.Fatalf("first operation failed before receipt: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("first operation did not reach subscription lock")
			}
			go func() { secondDone <- second() }()
			select {
			case err := <-secondDone:
				t.Fatalf("competing operation escaped subscription lock: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			unblock()
			require.NoError(t, <-firstDone)
			var account commerceschema.SubscriptionResetOpportunityAccount
			require.NoError(t, db.First(&account, "user_id = ?", user.Id).Error)
			if firstRefresh {
				require.ErrorIs(t, <-secondDone, commerceschema.ErrSubscriptionClaudeConversionResetUsed)
				require.Zero(t, account.AvailableTotal)
			} else {
				require.ErrorIs(t, <-secondDone, commerceschema.ErrSubscriptionResetOpportunityConverted)
				require.Equal(t, 1, account.AvailableTotal)
			}
		})
	}
	t.Run("refresh SQL failure leaves subscription and opportunity untouched", func(t *testing.T) {
		user, _, sub := seed(t)
		used := quotaUnitsFromUSD(20)
		require.NoError(t, db.Model(&sub).Update("amount_used", used).Error)
		require.NoError(t, db.Create(&commerceschema.SubscriptionResetOpportunityAccount{UserId: user.Id, EarnedTotal: 1, AvailableTotal: 1}).Error)
		const callback = "test:refresh-sql-rollback"
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if ledger, ok := tx.Statement.Dest.(*commerceschema.SubscriptionResetOpportunityLedger); ok && ledger.UserId == user.Id && ledger.ChangeType == "use" {
				tx.AddError(tx.Exec("SELECT 1 / 0").Error)
			}
		}))
		t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
		_, err := UseUserSubscriptionResetOpportunity(user.Id)
		require.ErrorContains(t, err, "division by zero")
		var account commerceschema.SubscriptionResetOpportunityAccount
		require.NoError(t, db.First(&account, "user_id = ?", user.Id).Error)
		require.Equal(t, 1, account.AvailableTotal)
		require.Zero(t, account.UsedTotal)
		require.Empty(t, account.LastUsedMonth)
		var saved commerceschema.UserSubscription
		require.NoError(t, db.First(&saved, sub.Id).Error)
		require.Equal(t, used, saved.AmountUsed)
		var count int64
		require.NoError(t, db.Model(&billingschema.BillingAccount{}).Where("owner_type = ? AND owner_id = ?", "user_subscription", sub.Id).Count(&count).Error)
		require.Zero(t, count)
	})
	if !t.Failed() {
		fmt.Println("PASS: PostgreSQL 12-caller conversion/redeem idempotency, no overspend, SQL rollback, refund lock interleaving and delayed gateway projection")
	}
}
