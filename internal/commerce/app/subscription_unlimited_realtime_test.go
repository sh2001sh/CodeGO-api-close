package app

import (
	"errors"
	"math"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	"github.com/sh2001sh/new-api/dto"
	billingapp "github.com/sh2001sh/new-api/internal/billing/app"
	billingdomain "github.com/sh2001sh/new-api/internal/billing/domain"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func unlimitedRealtimeFixture(t *testing.T, tokenQuota int) (*gorm.DB, *commerceschema.UserSubscription, *gin.Context, *relaycommon.RelayInfo, *billingapp.BillingSession) {
	t.Helper()
	db, sub, _ := adminQuotaFixture(t, 0, 0, false)
	ensureSubscriptionPreConsumeRecordSchema(t)
	require.NoError(t, db.AutoMigrate(&identityschema.Token{}))
	insertSubscriptionStoreTestUser(t, sub.UserId, []int{sub.Id})
	require.NoError(t, db.Model(sub).Update("period_amount", 10000).Error)
	require.NoError(t, db.Model(sub).Update("model_limits", `{"gpt-5":10000}`).Error)
	token := &identityschema.Token{Id: 18504, UserId: sub.UserId, Key: "unlimited-realtime-token", RemainQuota: tokenQuota,
		Status: constant.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, db.Create(token).Error)
	policy := gatewaystore.SubscriptionGroupPolicy2JSONString()
	require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(`{"unlimited_realtime_test":{"enabled":true,"multiplier":1}}`))
	t.Cleanup(func() { require.NoError(t, gatewaystore.UpdateSubscriptionGroupPolicyByJSONString(policy)) })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UserId: sub.UserId, TokenId: token.Id, TokenKey: token.Key,
		RequestId: "unlimited-realtime-request", OriginModelName: "gpt-5", UsingGroup: "unlimited_realtime_test", ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "subscription_only"},
		PriceData:   types.PriceData{GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1}}}
	session, apiErr := billingapp.NewBillingSession(ctx, info, 100)
	require.Nil(t, apiErr)
	info.Billing = session
	require.Zero(t, info.SubscriptionAmountTotal)
	return db, sub, ctx, info, session
}

func assertUnlimitedUsage(t *testing.T, db *gorm.DB, sub *commerceschema.UserSubscription, info *relaycommon.RelayInfo, used int64, tokenQuota int, status string) {
	t.Helper()
	var stored commerceschema.UserSubscription
	require.NoError(t, db.First(&stored, sub.Id).Error)
	require.Zero(t, stored.AmountTotal)
	require.Equal(t, used, stored.AmountUsed)
	require.Equal(t, used, stored.PeriodUsed)
	require.Equal(t, used, stored.GetModelUsageMap()["gpt-5"])
	var record commerceschema.SubscriptionPreConsumeRecord
	require.NoError(t, db.First(&record, "request_id = ?", info.RequestId).Error)
	require.Equal(t, status, record.Status)
	token, err := billingapp.GetTokenByID(info.TokenId)
	require.NoError(t, err)
	require.Equal(t, tokenQuota, token.RemainQuota)
	var account billingschema.BillingAccount
	query := db.Where("owner_type = ? AND owner_id = ?", "user_subscription", sub.Id).Limit(1).Find(&account)
	require.NoError(t, query.Error)
	if query.RowsAffected > 0 {
		var snapshot billingschema.BillingBalanceSnapshot
		require.NoError(t, db.First(&snapshot, "account_id = ?", account.AccountID).Error)
		require.Zero(t, snapshot.AvailableBalance)
		require.Zero(t, snapshot.ReservedBalance)
		require.Zero(t, snapshot.GrantedTotal)
		require.Zero(t, snapshot.ConsumedTotal)
		require.Zero(t, snapshot.RefundedTotal)
	}
	var holds int64
	require.NoError(t, db.Model(&billingschema.BillingReservation{}).Where("request_id = ?", info.RequestId).Count(&holds).Error)
	require.Zero(t, holds)
}

func TestUnlimitedRealtimeChunksSettleOnceWithoutFiniteFunding(t *testing.T) {
	db, sub, ctx, info, session := unlimitedRealtimeFixture(t, 2000)
	usage := &dto.RealtimeUsage{InputTokenDetails: dto.InputTokenDetails{TextTokens: 10}}
	ratio, _, _ := gatewaystore.GetModelRatio(info.OriginModelName)
	chunk := int(math.Round(10 * ratio * gatewaystore.GetGroupRatio(info.UsingGroup)))
	require.Positive(t, chunk)
	require.NoError(t, billingapp.PreWssConsumeQuota(ctx, info, usage))
	require.NoError(t, billingapp.PreWssConsumeQuota(ctx, info, usage))
	target := max(100, chunk*2)
	assertUnlimitedUsage(t, db, sub, info, int64(target), 2000-target, "consumed")
	require.NoError(t, ReserveUnlimitedSubscriptionQuotaTarget(info.RequestId, sub.Id, "gpt-5", int64(target)))
	actual := target - 20
	require.NoError(t, session.Settle(actual))
	require.NoError(t, session.Settle(actual))
	require.NoError(t, SettleSubscriptionReservation(info.RequestId, sub.Id, "gpt-5", int64(actual)))
	require.ErrorIs(t, SettleSubscriptionReservation(info.RequestId, sub.Id, "gpt-5", int64(actual+1)), billingdomain.ErrLedgerConflict)
	require.NoError(t, RefundSubscriptionPreConsume(info.RequestId))
	require.NoError(t, session.RefundSync(ctx))
	assertUnlimitedUsage(t, db, sub, info, int64(actual), 2000-actual, "settled")
	_, err := PreConsumeUserSubscription(info.RequestId, sub.UserId, "gpt-5", 100)
	require.Error(t, err)
	var refunds int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", "subscription-usage-adjust:relay-settle:"+info.RequestId).Count(&refunds).Error)
	require.EqualValues(t, 1, refunds)
}

func TestUnlimitedRealtimeTokenShortfallDoesNotGrowUsageHold(t *testing.T) {
	db, sub, ctx, info, session := unlimitedRealtimeFixture(t, 150)
	require.Error(t, session.Reserve(200))
	assertUnlimitedUsage(t, db, sub, info, 100, 50, "consumed")
	require.Error(t, session.Settle(200))
	require.False(t, info.BillingSettled)
	assertUnlimitedUsage(t, db, sub, info, 100, 50, "consumed")
	require.NoError(t, session.RefundSync(ctx))
	require.NoError(t, session.RefundSync(ctx))
	assertUnlimitedUsage(t, db, sub, info, 0, 150, "refunded")
}

func TestUnlimitedRealtimeFailuresRollbackAndCancelCanRetry(t *testing.T) {
	db, sub, ctx, info, session := unlimitedRealtimeFixture(t, 2000)
	const callback = "test:unlimited-usage-failure"
	installFailure := func() {
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "user_subscriptions" {
				tx.AddError(errors.New("injected unlimited projection failure"))
			}
		}))
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	installFailure()
	require.ErrorContains(t, session.Reserve(200), "injected unlimited projection failure")
	assertUnlimitedUsage(t, db, sub, info, 100, 1900, "consumed")
	require.NoError(t, db.Callback().Update().Remove(callback))
	require.NoError(t, session.Reserve(200))
	assertUnlimitedUsage(t, db, sub, info, 200, 1800, "consumed")
	installFailure()
	require.ErrorContains(t, session.Settle(180), "injected unlimited projection failure")
	assertUnlimitedUsage(t, db, sub, info, 200, 1800, "consumed")
	require.ErrorContains(t, session.RefundSync(ctx), "injected unlimited projection failure")
	assertUnlimitedUsage(t, db, sub, info, 200, 1800, "consumed")
	require.NoError(t, db.Callback().Update().Remove(callback))
	require.NoError(t, session.RefundSync(ctx))
	require.NoError(t, session.RefundSync(ctx))
	assertUnlimitedUsage(t, db, sub, info, 0, 2000, "refunded")
	var receipts int64
	require.NoError(t, db.Model(&billingschema.BillingLedgerEntry{}).Where("idempotency_key = ?", "subscription-usage-adjust:relay-cancel:"+info.RequestId).Count(&receipts).Error)
	require.EqualValues(t, 1, receipts)
}

func TestUnlimitedPublicAdditionalQuotaUpdatesRefundableTotal(t *testing.T) {
	db, sub, _, info, _ := unlimitedRealtimeFixture(t, 2000)
	require.NoError(t, ReserveAdditionalSubscriptionQuota(info.RequestId, sub.Id, "gpt-5", 50))
	require.NoError(t, ReserveUnlimitedSubscriptionQuotaTarget(info.RequestId, sub.Id, "gpt-5", 150))
	require.NoError(t, ReserveUnlimitedSubscriptionQuotaTarget(info.RequestId, sub.Id, "gpt-5", 200))
	require.NoError(t, RefundSubscriptionPreConsume(info.RequestId))
	require.NoError(t, RefundSubscriptionPreConsume(info.RequestId))
	// Direct funding APIs leave the independently owned token hold untouched.
	assertUnlimitedUsage(t, db, sub, info, 0, 1900, "refunded")
}
