package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

func atomicPackSession(t *testing.T, db *gorm.DB) (*billingapp.BillingSession, *relaycommon.RelayInfo, int, string, int) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&billingschema.FundingSourcePolicy{}, &billingschema.ReferralRebateProgram{}, &billingschema.ReferralConsumptionRebate{}, &marketplaceschema.Settlement{}))
	stamp := fmt.Sprint(time.Now().UnixNano())
	now := time.Now().Unix()
	inviter := identityschema.User{Username: "atomic-inviter-" + stamp, AffCode: "ai" + stamp, CreatedAt: now - 3600}
	require.NoError(t, db.Create(&inviter).Error)
	invitee := identityschema.User{Username: "atomic-invitee-" + stamp, AffCode: "ae" + stamp, InviterId: inviter.Id, CreatedAt: now - 60}
	require.NoError(t, db.Create(&invitee).Error)
	require.NoError(t, db.Clauses(clause.OnConflict{DoNothing: true}).Create(&billingschema.ReferralRebateProgram{ID: 1, RateBPS: 300, EffectiveAt: now - 3600}).Error)
	plan := defaultSubscriptionPlans()[0]
	plan.Id = 890000 + invitee.Id
	require.NoError(t, db.Create(&plan).Error)
	sub, err := createCreditPackBalanceTx(db, invitee.Id, plan.Id, 1100, 1000, 6000, "atomic-purchase:"+stamp)
	require.NoError(t, err)
	require.NoError(t, db.Create(&commerceschema.SubscriptionOrder{UserId: invitee.Id, PlanId: plan.Id, TradeNo: "atomic-order:" + stamp, Money: 1, Status: "success", TargetSubscriptionId: sub.Id, CreditPackQuota: 1100, CreditPackPaidQuota: 1000, CreditPackChannelShareBPS: 6000}).Error)
	info := &relaycommon.RelayInfo{UserId: invitee.Id, OriginModelName: "gpt-5", RequestId: "atomic-relay:" + stamp, IsPlayground: true, ForcePreConsume: true,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 123, ChannelScope: gatewayschema.ChannelScopeOfficial}, UserSetting: dto.UserSetting{BillingPreference: "subscription_only"}}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	session, apiErr := billingapp.NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	require.NotNil(t, session)
	// Two native reservations must commit or roll back together.
	require.NoError(t, session.Reserve(200))
	var account billingschema.BillingAccount
	require.NoError(t, db.First(&account, "owner_type = ? AND owner_id = ?", "user_subscription", sub.Id).Error)
	return session, info, sub.Id, account.AccountID, inviter.Id
}

func assertAtomicPackRolledBack(t *testing.T, db *gorm.DB, subID int, account string) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&billingschema.BillingSettlement{}).Where("reservation_id IN (?)", db.Model(&billingschema.BillingReservation{}).Select("reservation_id").Where("account_id = ?", account)).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&billingschema.BillingOutboxEvent{}).Where("account_id = ? AND event_type = ?", account, "billing.reservation_settled").Count(&count).Error)
	require.Zero(t, count)
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&snapshot, "account_id = ?", account).Error)
	require.EqualValues(t, 900, snapshot.AvailableBalance)
	require.EqualValues(t, 200, snapshot.ReservedBalance)
	require.Zero(t, snapshot.ConsumedTotal)
	var sub commerceschema.UserSubscription
	require.NoError(t, db.First(&sub, subID).Error)
	require.EqualValues(t, 200, sub.AmountUsed)
}

func assertAtomicPackCommitted(t *testing.T, db *gorm.DB, session *billingapp.BillingSession, info *relaycommon.RelayInfo, account string, inviterID int) {
	t.Helper()
	require.NoError(t, session.Settle(180))
	require.True(t, info.BillingSettled)
	var events []billingschema.BillingOutboxEvent
	require.NoError(t, db.Where("account_id = ? AND event_type = ?", account, "billing.reservation_settled").Find(&events).Error)
	require.Len(t, events, 2)
	for _, event := range events {
		var payload struct {
			Context struct {
				Original string   `json:"original_request_id"`
				Funding  string   `json:"funding_request_id"`
				IDs      []string `json:"settlement_ids"`
			} `json:"referral_context"`
		}
		require.NoError(t, json.Unmarshal(event.Payload, &payload))
		require.Equal(t, info.RequestId, payload.Context.Original)
		require.True(t, strings.HasPrefix(payload.Context.Funding, "cp:"))
		require.Len(t, payload.Context.IDs, 2)
	}
	// The async consumer has only committed database evidence; it cannot rely
	// on the live gateway/session to attach missing referral information.
	for i := 0; i < 3; i++ {
		_, err := billingapp.RunLedgerWorkerBatch(context.Background(), 100)
		require.NoError(t, err)
	}
	var inviter identityschema.User
	require.NoError(t, db.First(&inviter, inviterID).Error)
	require.Equal(t, 4, inviter.ClaudeQuota) // floor(180 * 1000/1100 * 3%).
	var snapshot billingschema.BillingBalanceSnapshot
	require.NoError(t, db.First(&snapshot, "account_id = ?", account).Error)
	require.EqualValues(t, 920, snapshot.AvailableBalance)
	require.Zero(t, snapshot.ReservedBalance)
	require.EqualValues(t, 180, snapshot.ConsumedTotal)
}

func TestCreditPackAtomicSettlementRollsBackPersistenceFailureAndPanic(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprintf("panic_%t", crash), func(t *testing.T) {
			db := setupRedemptionTestDB(t)
			ensureSubscriptionPreConsumeRecordSchema(t)
			session, info, subID, account, inviter := atomicPackSession(t, db)
			const callback = "test:fail-referral-evidence"
			require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
				if tx.Statement.Table == (billingschema.BillingOutboxEvent{}).TableName() {
					if crash {
						panic("injected gateway exit before referral persistence")
					}
					tx.AddError(errors.New("injected referral persistence failure"))
				}
			}))
			if crash {
				require.Panics(t, func() { _ = session.Settle(180) })
			} else {
				require.ErrorContains(t, session.Settle(180), "injected referral persistence failure")
			}
			require.False(t, info.BillingSettled)
			assertAtomicPackRolledBack(t, db, subID, account)
			require.NoError(t, db.Callback().Update().Remove(callback))
			assertAtomicPackCommitted(t, db, session, info, account, inviter)
		})
	}
}

func TestCreditPackAtomicSettlementPostgresVisibilityAndFailure(t *testing.T) {
	dsn := os.Getenv("CREDIT_PACK_TEST_DSN")
	if dsn == "" {
		t.Skip("CREDIT_PACK_TEST_DSN is not configured")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	var name string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	require.True(t, strings.HasPrefix(name, "codego_release_atomic_"), "dedicated atomic-settlement test database required")
	oldDB, oldLog := platformdb.DB, platformdb.LogDB
	oldPG, oldSQLite, oldMySQL, oldRedis := platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled
	platformdb.DB, platformdb.LogDB = db, db
	platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = true, false, false, false
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqlDB.Close()
		platformdb.DB, platformdb.LogDB = oldDB, oldLog
		platformdb.UsingPostgreSQL, platformdb.UsingSQLite, platformdb.UsingMySQL, platformcache.RedisEnabled = oldPG, oldSQLite, oldMySQL, oldRedis
	})
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS billing").Error)
	require.NoError(t, db.Exec("CREATE SCHEMA IF NOT EXISTS marketplace").Error)
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &commerceschema.SubscriptionPlan{}, &commerceschema.SubscriptionOrder{}, &commerceschema.UserSubscription{}, &commerceschema.SubscriptionPreConsumeRecord{}, &commerceschema.TopUp{},
		&commerceschema.SubscriptionResetOpportunityAccount{}, &commerceschema.SubscriptionResetOpportunityLedger{},
		&billingschema.BillingAccount{}, &billingschema.BillingBalanceSnapshot{}, &billingschema.BillingLedgerEntry{}, &billingschema.BillingReservation{}, &billingschema.BillingSettlement{}, &billingschema.BillingOutboxEvent{}, &billingschema.FundingLot{}, &billingschema.FundingAllocation{}))
	session, info, subID, account, inviter := atomicPackSession(t, db)
	const callback = "test:postgres-referral-evidence"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "outbox_events" {
			// A PostgreSQL error after native settlement must abort the whole Tx.
			tx.AddError(tx.Exec("SELECT 1 / 0").Error)
		}
	}))
	require.ErrorContains(t, session.Settle(180), "division by zero")
	require.False(t, info.BillingSettled)
	require.NoError(t, db.Callback().Update().Remove(callback))
	assertAtomicPackRolledBack(t, db, subID, account)
	entered, release := make(chan struct{}), make(chan struct{})
	first := true
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "outbox_events" && first {
			first = false
			close(entered)
			<-release
		}
	}))
	done := make(chan error, 1)
	go func() { done <- session.Settle(180) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("settlement never reached referral persistence")
	}
	// This independent connection observes no committed settlement or event
	// while the gateway is paused in the former crash window.
	var count int64
	err = db.Model(&billingschema.BillingSettlement{}).Where("reservation_id IN (?)", db.Model(&billingschema.BillingReservation{}).Select("reservation_id").Where("account_id = ?", account)).Count(&count).Error
	close(release)
	require.NoError(t, err)
	require.Zero(t, count)
	require.NoError(t, <-done)
	require.NoError(t, db.Callback().Update().Remove(callback))
	assertAtomicPackCommitted(t, db, session, info, account, inviter)
	fmt.Println("PASS: PostgreSQL native two-reservation settlement and referral evidence roll back on SQL failure; concurrent reader sees zero settlements until complete context commits; async referral applies once")
}
