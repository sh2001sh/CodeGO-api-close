package settlement

import (
	"context"
	"testing"
	"time"

	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestReferralPendingDoesNotStarveAutomaticRelease(t *testing.T) {
	db := openSettlementTestDB(t)
	require.NoError(t, Record(RecordParams{RequestID: "blocked-pack", GroupID: "group", OwnerUserID: 1, ConsumerUserID: 2, SettlementGrossAmount: 1100, ConsumerDebitAmount: 1100, CreditPackGrantedQuota: 1100, CreditPackPaidQuota: 1000, CreditPackChannelShareBPS: 6000}))
	require.NoError(t, Record(RecordParams{RequestID: "ordinary", GroupID: "group", OwnerUserID: 3, ConsumerUserID: 2, SettlementGrossAmount: 100, ConsumerDebitAmount: 100}))
	now := time.Now().UTC()
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Where("request_id = ?", "blocked-pack").Update("available_at", now.Add(-2*time.Hour)).Error)
	require.NoError(t, db.Model(&marketplaceschema.Settlement{}).Where("request_id = ?", "ordinary").Update("available_at", now.Add(-time.Hour)).Error)
	var released int64
	RegisterReleaseHook(func(_ *gorm.DB, owner int, amount int64, _, _ string) error {
		require.Equal(t, 3, owner)
		released += amount
		return nil
	})
	t.Cleanup(func() { RegisterReleaseHook(nil) })
	count, err := releaseDue(context.Background(), 1, 1)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.EqualValues(t, 95, released)
	var item marketplaceschema.Settlement
	require.NoError(t, db.First(&item, "request_id = ?", "blocked-pack").Error)
	require.Equal(t, statusPending, item.Status)
	require.ErrorContains(t, releaseOne(item.ID), "referral allocation is pending")
	called := false
	RegisterForfeitHook(func(_ *gorm.DB, _ string, _ int, _ int64, _ string) error { called = true; return nil })
	t.Cleanup(func() { RegisterForfeitHook(nil) })
	require.ErrorContains(t, forfeitOne(item.ID), "referral allocation is pending")
	require.False(t, called)
}
