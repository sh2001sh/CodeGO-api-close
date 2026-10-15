package app

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/dto"
	billingschema "github.com/sh2001sh/new-api/internal/billing/schema"
	relaycommon "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/sh2001sh/new-api/types"
	"github.com/stretchr/testify/require"
)

func newWalletRetryTestInfo(userID, tokenID int, tokenKey, requestID string) (*gin.Context, *relaycommon.RelayInfo) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	return ctx, &relaycommon.RelayInfo{
		UserId: userID, TokenId: tokenID, TokenKey: tokenKey, OriginModelName: "gpt-5",
		RequestId: requestID, IsPlayground: true, ForcePreConsume: true,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
}

func TestWalletBillingReservationIncreasesAcrossChannelRetryAndSettles(t *testing.T) {
	truncate(t)
	seedUser(t, 1020, 10_000)
	seedToken(t, 2020, 1020, "sk-wallet-retry-settle", 10_000)
	ctx, info := newWalletRetryTestInfo(1020, 2020, "sk-wallet-retry-settle", "req-wallet-retry-settle")

	require.Nil(t, PreConsumeRelayBilling(ctx, 3_000, info))
	require.Nil(t, PreConsumeRelayBilling(ctx, 4_500, info))
	require.Equal(t, 4_500, info.Billing.GetPreConsumedQuota())
	require.NoError(t, info.Billing.Settle(4_000))

	quota, err := GetUserClaudeWalletQuota(1020)
	require.NoError(t, err)
	require.Equal(t, 6_000, quota)
	snapshot := loadBillingSnapshot(t, 1020, "claude_wallet")
	require.Equal(t, int64(6_000), snapshot.AvailableBalance)
	require.Equal(t, int64(0), snapshot.ReservedBalance)
	require.Equal(t, int64(4_000), snapshot.ConsumedTotal)

	funding := info.Billing.(*BillingSession).funding.(*LedgerRelayFunding)
	var reservation billingschema.BillingReservation
	require.NoError(t, platformdb.DB.Where("reservation_id = ?", funding.ReservationID()).First(&reservation).Error)
	require.Equal(t, int64(4_500), reservation.ReservedAmount)
}

func TestWalletBillingReservationIncreaseRefundsEntireHold(t *testing.T) {
	truncate(t)
	seedUser(t, 1021, 10_000)
	seedToken(t, 2021, 1021, "sk-wallet-retry-refund", 10_000)
	ctx, info := newWalletRetryTestInfo(1021, 2021, "sk-wallet-retry-refund", "req-wallet-retry-refund")

	require.Nil(t, PreConsumeRelayBilling(ctx, 3_000, info))
	require.Nil(t, PreConsumeRelayBilling(ctx, 4_500, info))
	require.NoError(t, RefundRelayBillingSync(ctx, info))

	quota, err := GetUserClaudeWalletQuota(1021)
	require.NoError(t, err)
	require.Equal(t, 10_000, quota)
	snapshot := loadBillingSnapshot(t, 1021, "claude_wallet")
	require.Equal(t, int64(10_000), snapshot.AvailableBalance)
	require.Equal(t, int64(0), snapshot.ReservedBalance)
}

func TestWalletBillingReservationIncreaseReportsInsufficientQuota(t *testing.T) {
	truncate(t)
	seedUser(t, 1022, 4_000)
	seedToken(t, 2022, 1022, "sk-wallet-retry-insufficient", 10_000)
	ctx, info := newWalletRetryTestInfo(1022, 2022, "sk-wallet-retry-insufficient", "req-wallet-retry-insufficient")

	require.Nil(t, PreConsumeRelayBilling(ctx, 3_000, info))
	apiErr := PreConsumeRelayBilling(ctx, 4_500, info)
	require.NotNil(t, apiErr)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	require.NoError(t, RefundRelayBillingSync(ctx, info))

	quota, err := GetUserClaudeWalletQuota(1022)
	require.NoError(t, err)
	require.Equal(t, 4_000, quota)
}
