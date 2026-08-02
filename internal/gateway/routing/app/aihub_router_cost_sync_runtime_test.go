package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAIHubRouterRouteSnapshotAcceptsLatestLiveCycle(t *testing.T) {
	now := time.Date(2026, time.July, 31, 10, 30, 0, 0, time.UTC)
	content := strings.Join([]string{
		`{"eventType":"startup"}`,
		aiHubRouterCycleLog(now.Add(-time.Minute), false, 48, 0.01),
	}, "\n")

	snapshot, err := parseAIHubRouterRouteSnapshot([]byte(content), 50, now, 3*time.Minute, 0.10)
	if err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	if snapshot.ChannelID != 50 || snapshot.GroupID != 48 || snapshot.Multiplier != 0.01 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestParseAIHubRouterRouteSnapshotRejectsStaleCycle(t *testing.T) {
	now := time.Date(2026, time.July, 31, 10, 30, 0, 0, time.UTC)
	_, err := parseAIHubRouterRouteSnapshot(
		[]byte(aiHubRouterCycleLog(now.Add(-4*time.Minute), false, 48, 0.01)),
		50,
		now,
		3*time.Minute,
		0.10,
	)
	if err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale error, got %v", err)
	}
}

func TestParseAIHubRouterRouteSnapshotRejectsDryRun(t *testing.T) {
	now := time.Date(2026, time.July, 31, 10, 30, 0, 0, time.UTC)
	_, err := parseAIHubRouterRouteSnapshot(
		[]byte(aiHubRouterCycleLog(now.Add(-time.Minute), true, 48, 0.01)),
		50,
		now,
		3*time.Minute,
		0.10,
	)
	if err == nil || !strings.Contains(err.Error(), "dry run") {
		t.Fatalf("expected dry run error, got %v", err)
	}
}

func TestParseAIHubRouterRouteSnapshotRejectsMultiplierOutsideLimit(t *testing.T) {
	now := time.Date(2026, time.July, 31, 10, 30, 0, 0, time.UTC)
	_, err := parseAIHubRouterRouteSnapshot(
		[]byte(aiHubRouterCycleLog(now.Add(-time.Minute), false, 48, 0.1001)),
		50,
		now,
		3*time.Minute,
		0.10,
	)
	if err == nil || !strings.Contains(err.Error(), "outside the allowed range") {
		t.Fatalf("expected multiplier range error, got %v", err)
	}
}

func TestLoadAIHubRouterCostSyncSourcesRejectsDuplicateChannel(t *testing.T) {
	logsDir := t.TempDir()
	raw := fmt.Sprintf(`[
        {"channel_id":50,"log_file":%q},
        {"channel_id":50,"log_file":%q}
    ]`, filepath.Join(logsDir, "key11016.jsonl"), filepath.Join(logsDir, "key11239.jsonl"))
	_, err := loadAIHubRouterCostSyncSources(raw)
	if err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("expected duplicate channel error, got %v", err)
	}
}

func TestAIHubRouterCostSyncMaxMultiplierCapsAtPointOne(t *testing.T) {
	t.Setenv("AIHUB_ROUTER_COST_SYNC_MAX_MULTIPLIER", "0.12")
	if got := aiHubRouterCostSyncMaxMultiplier(); got != 0.10 {
		t.Fatalf("expected configured upper bound to cap at 0.10, got %.4f", got)
	}

	t.Setenv("AIHUB_ROUTER_COST_SYNC_MAX_MULTIPLIER", "0.10")
	if got := aiHubRouterCostSyncMaxMultiplier(); got != 0.10 {
		t.Fatalf("expected 0.10 to remain valid, got %.4f", got)
	}
}

func aiHubRouterCycleLog(completedAt time.Time, dryRun bool, groupID int64, multiplier float64) string {
	return fmt.Sprintf(
		`{"eventType":"routingCycle","dryRun":%t,"completedAt":%q,"decision":{"targetGroupId":%d,"multiplier":%.4f}}`,
		dryRun,
		completedAt.Format(time.RFC3339Nano),
		groupID,
		multiplier,
	)
}
