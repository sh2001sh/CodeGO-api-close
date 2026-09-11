package http

import (
	"encoding/csv"
	"strings"
	"testing"
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	"github.com/stretchr/testify/require"
)

func TestSecurityAuditCSVExportsVisibleEvidenceSafely(t *testing.T) {
	payload, err := securityAuditCSV([]gatewayschema.SecurityAuditEvent{{
		ID: "event-1", RequestID: "=HYPERLINK(\"https://example.test\")", CreatedAt: time.Unix(100, 0),
		Source: "prompt_guard", Severity: "high", RiskCode: "policy_violation",
		MarketplaceChannelID: "channel-a", UserID: 501, TokenName: "key-a", Model: "gpt-5.6",
		UpstreamErrorMessage: "blocked", UpstreamErrorBody: "raw body must not be exported",
		PromptPreview: "redacted preview", PromptHash: strings.Repeat("a", 64), ReviewStatus: "unreviewed",
	}})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(payload), "\xEF\xBB\xBF"))

	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(payload), "\xEF\xBB\xBF")))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, "'=HYPERLINK(\"https://example.test\")", records[1][1])
	require.Contains(t, records[1], "redacted preview")
	require.NotContains(t, string(payload), "raw body must not be exported")
}
