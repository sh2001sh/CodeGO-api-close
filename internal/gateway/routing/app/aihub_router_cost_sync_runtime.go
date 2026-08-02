package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
)

const (
	aiHubRouterCostSyncSourcesEnv      = "AIHUB_ROUTER_COST_SYNC_SOURCES"
	aiHubRouterCostSyncDefaultInterval = time.Minute
	aiHubRouterCostSyncDefaultMaxAge   = 3 * time.Minute
	// This is a validation ceiling for future AIHubRouter sync snapshots.
	// It does not rewrite existing route-pool member multipliers.
	aiHubRouterCostSyncDefaultMaxCost  = 0.10
	aiHubRouterCostSyncMaxLogTailBytes = 256 * 1024
	aiHubRouterCostSyncFutureClockSkew = time.Minute
	aiHubRouterCostSyncChangeEpsilon   = 0.000001
)

var aiHubRouterCostSyncTaskOnce sync.Once

type aiHubRouterCostSyncSource struct {
	ChannelID int    `json:"channel_id"`
	LogFile   string `json:"log_file"`
}

type aiHubRouterRouteSnapshot struct {
	ChannelID   int
	GroupID     int64
	Multiplier  float64
	CompletedAt time.Time
}

type aiHubRouterRoutingLogEvent struct {
	EventType   string    `json:"eventType"`
	DryRun      bool      `json:"dryRun"`
	CompletedAt time.Time `json:"completedAt"`
	Decision    struct {
		TargetGroupID int64   `json:"targetGroupId"`
		Multiplier    float64 `json:"multiplier"`
	} `json:"decision"`
}

// StartAIHubRouterCostSyncTask synchronizes configured AIHubRouter route costs
// from its local, structured watch logs. The task is disabled unless sources are
// explicitly configured, so regular route pools retain their manual costs.
func StartAIHubRouterCostSyncTask() {
	aiHubRouterCostSyncTaskOnce.Do(func() {
		if !platformconfig.IsMasterNode {
			return
		}

		sources, err := loadAIHubRouterCostSyncSources(os.Getenv(aiHubRouterCostSyncSourcesEnv))
		if err != nil {
			platformobservability.SysLog(fmt.Sprintf("AIHubRouter cost sync disabled: %v", err))
			return
		}
		if len(sources) == 0 {
			return
		}

		interval := aiHubRouterCostSyncInterval()
		maxAge := aiHubRouterCostSyncMaxAge()
		maxMultiplier := aiHubRouterCostSyncMaxMultiplier()
		go func() {
			platformobservability.SysLog(fmt.Sprintf(
				"AIHubRouter cost sync started: sources=%d interval=%s max_age=%s max_multiplier=%.4f",
				len(sources), interval, maxAge, maxMultiplier,
			))
			runAIHubRouterCostSyncTaskOnce(sources, maxAge, maxMultiplier, time.Now())
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for now := range ticker.C {
				runAIHubRouterCostSyncTaskOnce(sources, maxAge, maxMultiplier, now)
			}
		}()
	})
}

func loadAIHubRouterCostSyncSources(raw string) ([]aiHubRouterCostSyncSource, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var sources []aiHubRouterCostSyncSource
	if err := json.Unmarshal([]byte(raw), &sources); err != nil {
		return nil, fmt.Errorf("parse %s: %w", aiHubRouterCostSyncSourcesEnv, err)
	}
	if len(sources) == 0 {
		return nil, errors.New("AIHubRouter cost sync sources cannot be empty")
	}
	seenChannels := make(map[int]struct{}, len(sources))
	for index := range sources {
		sources[index].LogFile = strings.TrimSpace(sources[index].LogFile)
		if sources[index].ChannelID <= 0 {
			return nil, errors.New("AIHubRouter cost sync channel id must be positive")
		}
		if !filepath.IsAbs(sources[index].LogFile) {
			return nil, errors.New("AIHubRouter cost sync log file must be absolute")
		}
		if _, found := seenChannels[sources[index].ChannelID]; found {
			return nil, fmt.Errorf("AIHubRouter cost sync channel %d is configured more than once", sources[index].ChannelID)
		}
		seenChannels[sources[index].ChannelID] = struct{}{}
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].ChannelID < sources[j].ChannelID })
	return sources, nil
}

func runAIHubRouterCostSyncTaskOnce(
	sources []aiHubRouterCostSyncSource,
	maxAge time.Duration,
	maxMultiplier float64,
	now time.Time,
) {
	updates := make(map[int]float64, len(sources))
	for _, source := range sources {
		snapshot, err := readAIHubRouterRouteSnapshot(source, now, maxAge, maxMultiplier)
		if err != nil {
			platformobservability.SysLog(fmt.Sprintf(
				"AIHubRouter cost sync skipped: channel_id=%d err=%v", source.ChannelID, err,
			))
			continue
		}
		updates[snapshot.ChannelID] = snapshot.Multiplier
	}
	if len(updates) == 0 {
		return
	}

	changed, missing, err := gatewaystore.UpdateRoutePoolMemberCostMultipliers(updates, aiHubRouterCostSyncChangeEpsilon)
	if err != nil {
		platformobservability.SysLog(fmt.Sprintf("AIHubRouter cost sync update failed: %v", err))
		return
	}
	if changed > 0 || len(missing) > 0 {
		platformobservability.SysLog(fmt.Sprintf(
			"AIHubRouter cost sync completed: updated_members=%d missing_channels=%v", changed, missing,
		))
	}
}

func readAIHubRouterRouteSnapshot(
	source aiHubRouterCostSyncSource,
	now time.Time,
	maxAge time.Duration,
	maxMultiplier float64,
) (aiHubRouterRouteSnapshot, error) {
	content, err := readTail(source.LogFile, aiHubRouterCostSyncMaxLogTailBytes)
	if err != nil {
		return aiHubRouterRouteSnapshot{}, err
	}
	return parseAIHubRouterRouteSnapshot(content, source.ChannelID, now, maxAge, maxMultiplier)
}

func readTail(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	offset := max(info.Size()-maxBytes, 0)
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(file, maxBytes))
}

func parseAIHubRouterRouteSnapshot(
	content []byte,
	channelID int,
	now time.Time,
	maxAge time.Duration,
	maxMultiplier float64,
) (aiHubRouterRouteSnapshot, error) {
	lines := strings.Split(string(content), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}

		var event aiHubRouterRoutingLogEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.EventType != "routingCycle" {
			continue
		}
		if event.DryRun {
			return aiHubRouterRouteSnapshot{}, errors.New("latest routing cycle is a dry run")
		}
		if event.CompletedAt.IsZero() {
			return aiHubRouterRouteSnapshot{}, errors.New("routing cycle completion time is missing")
		}
		age := now.Sub(event.CompletedAt)
		if age > maxAge {
			return aiHubRouterRouteSnapshot{}, fmt.Errorf("routing cycle is stale: age=%s", age.Round(time.Second))
		}
		if age < -aiHubRouterCostSyncFutureClockSkew {
			return aiHubRouterRouteSnapshot{}, fmt.Errorf("routing cycle is too far in the future: age=%s", age.Round(time.Second))
		}
		if event.Decision.TargetGroupID <= 0 {
			return aiHubRouterRouteSnapshot{}, errors.New("routing cycle target group is missing")
		}
		if event.Decision.Multiplier <= 0 || event.Decision.Multiplier > maxMultiplier || math.IsNaN(event.Decision.Multiplier) || math.IsInf(event.Decision.Multiplier, 0) {
			return aiHubRouterRouteSnapshot{}, fmt.Errorf("routing cycle multiplier %.6f is outside the allowed range", event.Decision.Multiplier)
		}
		return aiHubRouterRouteSnapshot{
			ChannelID:   channelID,
			GroupID:     event.Decision.TargetGroupID,
			Multiplier:  event.Decision.Multiplier,
			CompletedAt: event.CompletedAt,
		}, nil
	}
	return aiHubRouterRouteSnapshot{}, errors.New("no routing cycle found in log tail")
}

func aiHubRouterCostSyncInterval() time.Duration {
	seconds := platformconfig.GetEnvOrDefaultInt("AIHUB_ROUTER_COST_SYNC_INTERVAL_SECONDS", int(aiHubRouterCostSyncDefaultInterval.Seconds()))
	if seconds < 30 {
		seconds = int(aiHubRouterCostSyncDefaultInterval.Seconds())
	}
	return time.Duration(seconds) * time.Second
}

func aiHubRouterCostSyncMaxAge() time.Duration {
	seconds := platformconfig.GetEnvOrDefaultInt("AIHUB_ROUTER_COST_SYNC_MAX_AGE_SECONDS", int(aiHubRouterCostSyncDefaultMaxAge.Seconds()))
	if seconds < int(aiHubRouterCostSyncDefaultInterval.Seconds()) {
		seconds = int(aiHubRouterCostSyncDefaultMaxAge.Seconds())
	}
	return time.Duration(seconds) * time.Second
}

func aiHubRouterCostSyncMaxMultiplier() float64 {
	raw := strings.TrimSpace(os.Getenv("AIHUB_ROUTER_COST_SYNC_MAX_MULTIPLIER"))
	if raw == "" {
		return aiHubRouterCostSyncDefaultMaxCost
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 || value > aiHubRouterCostSyncDefaultMaxCost || math.IsNaN(value) || math.IsInf(value, 0) {
		platformobservability.SysLog(fmt.Sprintf("invalid AIHUB_ROUTER_COST_SYNC_MAX_MULTIPLIER=%q; using %.4f", raw, aiHubRouterCostSyncDefaultMaxCost))
		return aiHubRouterCostSyncDefaultMaxCost
	}
	return value
}
