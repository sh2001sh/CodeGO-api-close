package runtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"
	platformcache "github.com/sh2001sh/new-api/internal/platform/cache"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

const (
	channelConcurrencyInstanceTTL       = 15 * time.Second
	channelConcurrencyHeartbeatInterval = 5 * time.Second
	channelConcurrencyPublishDebounce   = 100 * time.Millisecond
	channelConcurrencyRedisTimeout      = 750 * time.Millisecond
	channelConcurrencyErrorLogInterval  = time.Minute
	channelConcurrencyIndexKey          = "gateway:channel-concurrency:v1:instances"
	channelConcurrencyInstanceKeyPrefix = "gateway:channel-concurrency:v1:instance:"
)

var (
	channelConcurrencyInstanceID = platformruntime.GetUUID()
	channelConcurrencyWake       = make(chan struct{}, 1)
	channelConcurrencyStart      sync.Once
	channelConcurrencyLastError  atomic.Int64
)

func notifyChannelConcurrencyChanged() {
	if !platformcache.RedisReady() {
		return
	}
	channelConcurrencyStart.Do(func() {
		go runChannelConcurrencyPublisher()
	})
	select {
	case channelConcurrencyWake <- struct{}{}:
	default:
	}
}

func runChannelConcurrencyPublisher() {
	ticker := time.NewTicker(channelConcurrencyHeartbeatInterval)
	defer ticker.Stop()
	var publishTimer *time.Timer
	var publishTimerC <-chan time.Time
	for {
		select {
		case <-channelConcurrencyWake:
			if publishTimer == nil {
				publishTimer = time.NewTimer(channelConcurrencyPublishDebounce)
				publishTimerC = publishTimer.C
			}
			continue
		case <-publishTimerC:
			publishTimer = nil
			publishTimerC = nil
		case <-ticker.C:
		}
		if err := publishChannelConcurrencySnapshot(snapshotLocalChannelConcurrency()); err != nil {
			reportChannelConcurrencyTelemetryError(err)
		}
	}
}

func publishChannelConcurrencySnapshot(snapshot map[int]int) error {
	if !platformcache.RedisReady() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyRedisTimeout)
	defer cancel()

	instanceKey := channelConcurrencyInstanceKey(channelConcurrencyInstanceID)
	pipe := platformcache.RDB.TxPipeline()
	pipe.Del(ctx, instanceKey)
	if len(snapshot) == 0 {
		pipe.SRem(ctx, channelConcurrencyIndexKey, channelConcurrencyInstanceID)
	} else {
		values := make(map[string]interface{}, len(snapshot))
		for channelID, active := range snapshot {
			values[strconv.Itoa(channelID)] = active
		}
		pipe.HSet(ctx, instanceKey, values)
		pipe.Expire(ctx, instanceKey, channelConcurrencyInstanceTTL)
		pipe.SAdd(ctx, channelConcurrencyIndexKey, channelConcurrencyInstanceID)
		pipe.Expire(ctx, channelConcurrencyIndexKey, time.Hour)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("publish channel concurrency snapshot: %w", err)
	}
	return nil
}

func sharedActiveChannelRequestsForChannels(channelIDs []int, local map[int]int) (map[int]int, bool) {
	if !platformcache.RedisReady() {
		return nil, false
	}
	result := make(map[int]int, len(channelIDs))
	fields := make([]string, 0, len(channelIDs))
	normalizedIDs := make([]int, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		if _, exists := result[channelID]; exists {
			continue
		}
		result[channelID] = 0
		normalizedIDs = append(normalizedIDs, channelID)
		fields = append(fields, strconv.Itoa(channelID))
	}
	if len(fields) == 0 {
		return result, true
	}

	ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyRedisTimeout)
	defer cancel()
	instanceIDs, err := platformcache.RDB.SMembers(ctx, channelConcurrencyIndexKey).Result()
	if err != nil {
		reportChannelConcurrencyTelemetryError(fmt.Errorf("list channel concurrency instances: %w", err))
		return nil, false
	}

	pipe := platformcache.RDB.Pipeline()
	commands := make(map[string]*redis.SliceCmd, len(instanceIDs))
	existsCommands := make(map[string]*redis.IntCmd, len(instanceIDs))
	for _, instanceID := range instanceIDs {
		instanceKey := channelConcurrencyInstanceKey(instanceID)
		commands[instanceID] = pipe.HMGet(ctx, instanceKey, fields...)
		existsCommands[instanceID] = pipe.Exists(ctx, instanceKey)
	}
	if _, err = pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		reportChannelConcurrencyTelemetryError(fmt.Errorf("load channel concurrency snapshots: %w", err))
		return nil, false
	}

	staleInstances := make([]interface{}, 0)
	currentInstanceSeen := false
	for instanceID, command := range commands {
		if instanceID == channelConcurrencyInstanceID {
			currentInstanceSeen = true
			for _, channelID := range normalizedIDs {
				result[channelID] += local[channelID]
			}
			continue
		}
		values, commandErr := command.Result()
		if commandErr != nil && !errors.Is(commandErr, redis.Nil) {
			reportChannelConcurrencyTelemetryError(fmt.Errorf("read channel concurrency snapshot: %w", commandErr))
			return nil, false
		}
		exists, existsErr := existsCommands[instanceID].Result()
		if existsErr != nil && !errors.Is(existsErr, redis.Nil) {
			reportChannelConcurrencyTelemetryError(fmt.Errorf("check channel concurrency snapshot: %w", existsErr))
			return nil, false
		}
		if exists == 0 {
			staleInstances = append(staleInstances, instanceID)
			continue
		}
		for index, raw := range values {
			if raw == nil {
				continue
			}
			active, parseErr := strconv.Atoi(fmt.Sprint(raw))
			if parseErr == nil && active > 0 {
				result[normalizedIDs[index]] += active
			}
		}
	}
	if !currentInstanceSeen {
		for _, channelID := range normalizedIDs {
			result[channelID] += local[channelID]
		}
	}
	if len(staleInstances) > 0 {
		if err := platformcache.RDB.SRem(ctx, channelConcurrencyIndexKey, staleInstances...).Err(); err != nil {
			reportChannelConcurrencyTelemetryError(fmt.Errorf("remove stale channel concurrency instances: %w", err))
		}
	}
	return result, true
}

func channelConcurrencyInstanceKey(instanceID string) string {
	return channelConcurrencyInstanceKeyPrefix + instanceID
}

func reportChannelConcurrencyTelemetryError(err error) {
	now := time.Now().UnixNano()
	last := channelConcurrencyLastError.Load()
	if last > 0 && time.Duration(now-last) < channelConcurrencyErrorLogInterval {
		return
	}
	if channelConcurrencyLastError.CompareAndSwap(last, now) {
		platformobservability.SysError("channel concurrency telemetry: " + err.Error())
	}
}
