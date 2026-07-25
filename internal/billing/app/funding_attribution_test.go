package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sh2001sh/new-api/internal/billing/schema"
	gatewayruntime "github.com/sh2001sh/new-api/internal/gateway/runtime"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestDailyFundingEconomicsEmptySourcesSerializesAsArray(t *testing.T) {
	report, err := DailyFundingEconomics(time.Now(), 1)
	require.NoError(t, err)

	payload, err := json.Marshal(report)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"sources":[]`)
}

func TestRecordRequestEconomicsUpsertsRepeatedRequest(t *testing.T) {
	truncate(t)
	info := &gatewayruntime.RelayInfo{
		RequestId:                 "request-economics-upsert",
		RoutePoolID:               1,
		ProcurementCostMultiplier: 0.08,
		ChannelMeta:               &gatewayruntime.ChannelMeta{ChannelId: 39},
	}
	require.NoError(t, RecordRequestEconomics(info, 100))

	info.RoutePoolID = 2
	info.ProcurementCostMultiplier = 0.15
	info.ChannelId = 51
	require.NoError(t, RecordRequestEconomics(info, 200))

	var records []billingschema.RequestEconomics
	require.NoError(t, platformdb.DB.Where("request_id = ?", info.RequestId).Find(&records).Error)
	require.Len(t, records, 1)
	require.Equal(t, 51, records[0].ChannelID)
	require.Equal(t, int64(2), records[0].RoutePoolID)
	require.Equal(t, int64(200), records[0].ActualAmount)
	require.InDelta(t, 0.15, records[0].ProcurementCostMultiplier, 0.0001)
}
