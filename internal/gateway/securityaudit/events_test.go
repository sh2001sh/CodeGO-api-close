package securityaudit

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestListAndUpdateEventsEnforceOwnerScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gatewayschema.SecurityAuditEvent{}))
	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })

	now := time.Now()
	events := []gatewayschema.SecurityAuditEvent{
		{ID: "owner-a", DedupeKey: "a", Source: EventSourcePromptGuard, Decision: "blocked", RiskCode: "prompt_guard", Severity: "high", OwnerUserID: 101, UserID: 1, MarketplaceChannelID: "channel-a", ReviewStatus: ReviewStatusUnreviewed, CreatedAt: now},
		{ID: "owner-b", DedupeKey: "b", Source: EventSourceUpstreamCyberPolicy, Decision: "blocked", RiskCode: "cyber_policy", Severity: "high", OwnerUserID: 202, UserID: 2, MarketplaceChannelID: "channel-b", ReviewStatus: ReviewStatusUnreviewed, CreatedAt: now},
	}
	require.NoError(t, db.Create(&events).Error)

	ownerResult, err := ListEvents(EventQuery{ViewerUserID: 101, Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(1), ownerResult.Total)
	require.Len(t, ownerResult.Items, 1)
	require.Equal(t, "owner-a", ownerResult.Items[0].ID)
	require.Equal(t, int64(1), ownerResult.Summary.AffectedChannels)

	exported, err := ExportEvents(EventQuery{ViewerUserID: 101, MarketplaceChannel: "channel-a"})
	require.NoError(t, err)
	require.Len(t, exported, 1)
	require.Equal(t, "owner-a", exported[0].ID)
	exported, err = ExportEvents(EventQuery{ViewerUserID: 101, MarketplaceChannel: "channel-b"})
	require.NoError(t, err)
	require.Empty(t, exported, "owner export must not expose another owner's channel")

	adminResult, err := ListEvents(EventQuery{ViewerUserID: 999, Admin: true, Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(2), adminResult.Total)

	_, err = UpdateEventReview(101, false, "owner-b", ReviewStatusResolved, "not mine")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	updated, err := UpdateEventReview(101, false, "owner-a", ReviewStatusAcknowledged, "reviewing")
	require.NoError(t, err)
	require.Equal(t, ReviewStatusAcknowledged, updated.ReviewStatus)
	require.Equal(t, 101, updated.ReviewedBy)
}

func TestEventDedupeKeyChangesWithEvidence(t *testing.T) {
	base := EventInput{RequestID: "req-1", Source: EventSourceUpstreamCyberPolicy, ChannelID: 8, RiskCode: "cyber_policy"}
	first := eventDedupeKey(base, "same body", "same prompt")
	require.Equal(t, first, eventDedupeKey(base, "same body", "same prompt"))
	require.NotEqual(t, first, eventDedupeKey(base, "different body", "same prompt"))
	require.NotEqual(t, first, eventDedupeKey(base, "same body", "different prompt"))
}

func TestNormalizeSeverityUsesSupportedLevels(t *testing.T) {
	require.Equal(t, "critical", normalizeSeverity(" CRITICAL "))
	require.Equal(t, "high", normalizeSeverity("unexpected"))
	require.Equal(t, "high", normalizeSeverity(""))
}

func TestListEventsCountsRecentTriggersWithinOwnerScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&gatewayschema.SecurityAuditEvent{}))
	originalDB := platformdb.DB
	platformdb.DB = db
	t.Cleanup(func() { platformdb.DB = originalDB })

	now := time.Now()
	events := []gatewayschema.SecurityAuditEvent{
		{ID: "recent-a-1", DedupeKey: "recent-a-1", Source: EventSourcePromptGuard, Decision: "blocked", RiskCode: "prompt_guard", Severity: "high", OwnerUserID: 101, UserID: 1, TokenID: 8, ReviewStatus: ReviewStatusUnreviewed, CreatedAt: now},
		{ID: "recent-a-2", DedupeKey: "recent-a-2", Source: EventSourcePromptGuard, Decision: "blocked", RiskCode: "prompt_guard", Severity: "high", OwnerUserID: 101, UserID: 1, TokenID: 8, ReviewStatus: ReviewStatusUnreviewed, CreatedAt: now.Add(-time.Hour)},
		{ID: "old-a", DedupeKey: "old-a", Source: EventSourcePromptGuard, Decision: "blocked", RiskCode: "prompt_guard", Severity: "high", OwnerUserID: 101, UserID: 1, TokenID: 8, ReviewStatus: ReviewStatusUnreviewed, CreatedAt: now.Add(-25 * time.Hour)},
		{ID: "recent-b", DedupeKey: "recent-b", Source: EventSourcePromptGuard, Decision: "blocked", RiskCode: "prompt_guard", Severity: "high", OwnerUserID: 202, UserID: 2, TokenID: 8, ReviewStatus: ReviewStatusUnreviewed, CreatedAt: now},
	}
	require.NoError(t, db.Create(&events).Error)

	result, err := ListEvents(EventQuery{ViewerUserID: 101, Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, result.Items, 3)
	for _, event := range result.Items {
		require.Equal(t, int64(2), event.RecentTriggerCount)
	}
}
