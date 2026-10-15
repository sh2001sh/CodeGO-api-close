package app

import (
	"testing"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func BenchmarkOwnerUsageLogs(b *testing.B) {
	db, logs := openOwnerUsageLogTestDB(b)
	channelID := 101
	require.NoError(b, db.Create(&marketplaceschema.Channel{ID: "bench", OwnerUserID: 10, InternalChannelID: &channelID}).Error)
	rows := make([]auditschema.Log, 30000)
	for i := range rows {
		rows[i] = auditschema.Log{ChannelId: channelID, Type: auditschema.LogTypeConsume, CreatedAt: int64(1700000000 + i*60), ModelName: "gpt-5.6"}
	}
	require.NoError(b, logs.CreateInBatches(rows, 200).Error)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := ListOwnerUsageLogs(10, OwnerUsageLogQuery{Page: 1, PageSize: 20})
		require.NoError(b, err)
		require.EqualValues(b, 30000, result.Total)
	}
}
