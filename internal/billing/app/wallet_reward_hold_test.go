package app

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestWalletRewardReleaseRatio(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	createdAt := now.Add(-12 * time.Hour).Unix()
	require.Equal(t, 0.0, walletRewardReleaseRatio(createdAt, now))
	require.InDelta(t, 0.0, walletRewardReleaseRatio(now.Add(-24*time.Hour).Unix(), now), 0.0001)
	require.InDelta(t, 0.5, walletRewardReleaseRatio(now.Add(-48*time.Hour).Unix(), now), 0.0001)
	require.Equal(t, 1.0, walletRewardReleaseRatio(now.Add(-72*time.Hour).Unix(), now))
	require.Equal(t, int64(50), unreleasedWalletRewardAmount(100, 0, now.Add(-48*time.Hour).Unix(), now))
	require.Equal(t, int64(25), unreleasedWalletRewardAmount(100, 25, now.Add(-48*time.Hour).Unix(), now))
}
