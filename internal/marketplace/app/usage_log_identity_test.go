package app

import (
	"encoding/json"
	"testing"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestEnrichUsageLogMarketplaceIdentity(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))
	require.NoError(t, db.Create(&[]marketplaceschema.Channel{
		{ID: "10001", ApprovedSourceLabel: "Codex Plus", SourceLabelStatus: marketplacedomain.SourceLabelApproved},
		{ID: "10002", ApprovedSourceLabel: "Codex Pro", SourceLabelStatus: marketplacedomain.SourceLabelApproved},
	}).Error)
	require.NoError(t, db.Create(&[]marketplaceschema.Group{
		{
			ID: "group-1", ChannelID: "10001", PublicSlug: "mg_codex_plus",
			SystemDisplayName: "stale-name", InternalGroupName: "Codex-Plus-ae381d", Multiplier: 0.8,
		},
		{
			ID: "group-2", ChannelID: "10002", PublicSlug: "mg_codex_pro",
			SystemDisplayName: "stale-name-2", InternalGroupName: "Codex-Pro-f81c2a", Multiplier: 1.5,
		},
	}).Error)

	logs := []*auditschema.Log{
		{Group: "Codex-Plus-ae381d", Other: `{"marketplace_group_id":"group-1","preserved":"value"}`},
		{Group: "Codex-Pro-f81c2a", Other: `{"marketplace_group_id":"group-2"}`},
		{Group: "default", Other: `{"group_ratio":1}`},
		nil,
	}

	require.NoError(t, EnrichUsageLogMarketplaceIdentity(logs))
	require.Equal(t, "Codex-Plus-ae381d", logs[0].Group)
	require.Equal(t, "Codex-Pro-f81c2a", logs[1].Group)

	first := decodeUsageLogOther(t, logs[0].Other)
	require.Equal(t, "group-1", first[logMarketplaceGroupIDKey])
	require.Equal(t, "Codex Plus-0.8x-10001", first[logMarketplaceGroupDisplayNameKey])
	require.Equal(t, "10001", first[logMarketplaceChannelIDKey])
	require.Equal(t, "mg_codex_plus", first[logMarketplacePublicSlugKey])
	require.Equal(t, "value", first["preserved"])

	second := decodeUsageLogOther(t, logs[1].Other)
	require.Equal(t, "Codex Pro-1.5x-10002", second[logMarketplaceGroupDisplayNameKey])
	require.JSONEq(t, `{"group_ratio":1}`, logs[2].Other)
}

func TestEnrichUsageLogMarketplaceIdentityIgnoresInvalidOrUnknownLogs(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}))

	logs := []*auditschema.Log{
		{Group: "default", Other: "not-json"},
		{Group: "legacy", Other: `{"marketplace_group_id":"missing"}`},
		{Group: "empty", Other: ""},
	}
	original := []string{logs[0].Other, logs[1].Other, logs[2].Other}

	require.NoError(t, EnrichUsageLogMarketplaceIdentity(logs))
	for index, log := range logs {
		require.Equal(t, original[index], log.Other)
	}
}

func decodeUsageLogOther(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	var other map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(raw), &other))
	return other
}
