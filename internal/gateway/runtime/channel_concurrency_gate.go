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
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

const (
	channelConcurrencyLeaseTTL          = 90 * time.Second
	channelConcurrencyRenewInterval     = 20 * time.Second
	channelConcurrencyGateRedisTimeout  = 2 * time.Second
	channelConcurrencyReserveAttempts   = 2
	channelConcurrencyRetryDelay        = 50 * time.Millisecond
	channelConcurrencyGateErrorInterval = time.Minute
	channelConcurrencyLeaseKeyPrefix    = "gateway:channel-concurrency:v2:"
)

var (
	channelConcurrencyGateLastError atomic.Int64
	channelConcurrencyReserveScript = redis.NewScript(`
local now = redis.call('TIME')
local now_ms = (tonumber(now[1]) * 1000) + math.floor(tonumber(now[2]) / 1000)
local expires_at = now_ms + tonumber(ARGV[2])
local total_limit = tonumber(ARGV[3])
local user_limit = tonumber(ARGV[4])

redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now_ms)
if user_limit > 0 then
  redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now_ms)
end

-- A timed-out client may retry after this script committed. Treat the same
-- token as the same lease so the retry never consumes a second slot.
if redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  redis.call('ZADD', KEYS[1], expires_at, ARGV[1])
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  if user_limit > 0 then
    redis.call('ZADD', KEYS[2], expires_at, ARGV[1])
    redis.call('PEXPIRE', KEYS[2], ARGV[2])
  end
  return 1
end

if total_limit > 0 and redis.call('ZCARD', KEYS[1]) >= total_limit then
  return 0
end
if user_limit > 0 and redis.call('ZCARD', KEYS[2]) >= user_limit then
  return 0
end

redis.call('ZADD', KEYS[1], expires_at, ARGV[1])
redis.call('PEXPIRE', KEYS[1], ARGV[2])
if user_limit > 0 then
  redis.call('ZADD', KEYS[2], expires_at, ARGV[1])
  redis.call('PEXPIRE', KEYS[2], ARGV[2])
end
return 1
`)
	channelConcurrencyRenewScript = redis.NewScript(`
local now = redis.call('TIME')
local now_ms = (tonumber(now[1]) * 1000) + math.floor(tonumber(now[2]) / 1000)
local expires_at = now_ms + tonumber(ARGV[2])
local renewed = 0

if redis.call('ZSCORE', KEYS[1], ARGV[1]) then
  redis.call('ZADD', KEYS[1], expires_at, ARGV[1])
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  renewed = renewed + 1
end
if tonumber(ARGV[4]) > 0 and redis.call('ZSCORE', KEYS[2], ARGV[1]) then
  redis.call('ZADD', KEYS[2], expires_at, ARGV[1])
  redis.call('PEXPIRE', KEYS[2], ARGV[2])
  renewed = renewed + 1
end
return renewed
`)
	channelConcurrencyReleaseScript = redis.NewScript(`
local removed = 0
removed = removed + redis.call('ZREM', KEYS[1], ARGV[1])
if redis.call('ZCARD', KEYS[1]) == 0 then redis.call('DEL', KEYS[1]) end
if tonumber(ARGV[2]) > 0 then
  removed = removed + redis.call('ZREM', KEYS[2], ARGV[1])
  if redis.call('ZCARD', KEYS[2]) == 0 then redis.call('DEL', KEYS[2]) end
end
return removed
`)
)

var runChannelConcurrencyReserve = func(
	ctx context.Context,
	keys []string,
	token string,
	totalLimit, userLimit int,
) (int, error) {
	return channelConcurrencyReserveScript.Run(
		ctx,
		platformcache.RDB,
		keys,
		token,
		channelConcurrencyLeaseTTL.Milliseconds(),
		totalLimit,
		userLimit,
	).Int()
}

// reserveRedisChannelConcurrency acquires one cross-Gateway lease. Redis being
	// disabled means the process-local gate remains authoritative. Redis is only
	// the cross-instance coordination layer; if it is temporarily unavailable,
	// keep serving through the local gate instead of turning every request into a
	// route-pool failure. Limits recover automatically when Redis is reachable.
func reserveRedisChannelConcurrency(
	channelID, userID, totalLimit, userLimit int,
) (func(), bool, ChannelConcurrencyAdmission) {
	if totalLimit <= 0 && userLimit <= 0 {
		return func() {}, false, ChannelConcurrencyAdmitted
	}
	if !platformcache.RedisEnabled {
		return func() {}, false, ChannelConcurrencyAdmitted
	}
	if platformcache.RDB == nil {
		reportChannelConcurrencyGateError(errors.New("redis is enabled but the client is unavailable"))
		return func() {}, false, ChannelConcurrencyAdmitted
	}

	token := channelConcurrencyLeaseToken()
	keys := channelConcurrencyLeaseKeys(channelID, userID)
	admitted, err := reserveChannelConcurrencyWithRetry(keys, token, totalLimit, userLimit)
	if err != nil {
		reportChannelConcurrencyGateError(fmt.Errorf("reserve channel %d lease: %w", channelID, err))
		return func() {}, false, ChannelConcurrencyAdmitted
	}
	if admitted != 1 {
		return func() {}, true, ChannelConcurrencyCapacityReached
	}

	stopRenewal := make(chan struct{})
	go renewChannelConcurrencyLease(stopRenewal, keys, token, channelID, totalLimit, userLimit)
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stopRenewal)
			releaseRedisChannelConcurrencyLease(keys, token, channelID, userLimit)
		})
	}, true, ChannelConcurrencyAdmitted
}

func reserveChannelConcurrencyWithRetry(
	keys []string,
	token string,
	totalLimit, userLimit int,
) (int, error) {
	attempts := platformconfig.GetEnvOrDefaultInt(
		"CHANNEL_CONCURRENCY_REDIS_RESERVE_ATTEMPTS",
		channelConcurrencyReserveAttempts,
	)
	if attempts < 1 {
		attempts = 1
	} else if attempts > 3 {
		attempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyGateOperationTimeout())
		admitted, err := runChannelConcurrencyReserve(ctx, keys, token, totalLimit, userLimit)
		cancel()
		if err == nil {
			return admitted, nil
		}
		lastErr = err
		if attempt+1 < attempts {
			time.Sleep(channelConcurrencyRetryDelay)
		}
	}
	return 0, lastErr
}

func channelConcurrencyGateOperationTimeout() time.Duration {
	timeoutMS := platformconfig.GetEnvOrDefaultInt(
		"CHANNEL_CONCURRENCY_REDIS_TIMEOUT_MS",
		int(channelConcurrencyGateRedisTimeout/time.Millisecond),
	)
	if timeoutMS < 100 {
		timeoutMS = 100
	} else if timeoutMS > 10000 {
		timeoutMS = 10000
	}
	return time.Duration(timeoutMS) * time.Millisecond
}

func renewChannelConcurrencyLease(stop <-chan struct{}, keys []string, token string, channelID, totalLimit, userLimit int) {
	ticker := time.NewTicker(channelConcurrencyRenewInterval)
	defer ticker.Stop()
	expectedRenewals := 1
	if userLimit > 0 {
		expectedRenewals++
	}
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyGateOperationTimeout())
		renewed, err := channelConcurrencyRenewScript.Run(
			ctx,
			platformcache.RDB,
			keys,
			token,
			channelConcurrencyLeaseTTL.Milliseconds(),
			totalLimit,
			userLimit,
		).Int()
		cancel()
		if err != nil {
			reportChannelConcurrencyGateError(fmt.Errorf("renew channel %d lease: %w", channelID, err))
			continue
		}
		if renewed != expectedRenewals {
			reportChannelConcurrencyGateError(fmt.Errorf("channel %d lease expired before renewal", channelID))
			return
		}
	}
}

func releaseRedisChannelConcurrencyLease(keys []string, token string, channelID, userLimit int) {
	ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyGateOperationTimeout())
	defer cancel()
	if _, err := channelConcurrencyReleaseScript.Run(
		ctx,
		platformcache.RDB,
		keys,
		token,
		userLimit,
	).Result(); err != nil {
		reportChannelConcurrencyGateError(fmt.Errorf("release channel %d lease: %w", channelID, err))
	}
}

func channelConcurrencyLeaseKeys(channelID, userID int) []string {
	channelTag := "{" + strconv.Itoa(channelID) + "}"
	base := channelConcurrencyLeaseKeyPrefix + channelTag
	return []string{base + ":all", base + ":user:" + strconv.Itoa(userID)}
}

func channelConcurrencyLeaseToken() string {
	return channelConcurrencyInstanceID + ":" + platformruntime.GetUUID()
}

// ActiveChannelRequestLeasesForChannels returns the live cross-Gateway lease
// count used by admission. It reports false when Redis cannot provide a shared
// snapshot so callers can fall back to process telemetry.
func ActiveChannelRequestLeasesForChannels(channelIDs []int) (map[int]int, bool) {
	if !platformcache.RedisReady() {
		return nil, false
	}
	result := make(map[int]int, len(channelIDs))
	uniqueIDs := make([]int, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		if _, exists := result[channelID]; exists {
			continue
		}
		result[channelID] = 0
		uniqueIDs = append(uniqueIDs, channelID)
	}
	if len(uniqueIDs) == 0 {
		return result, true
	}

	ctx, cancel := context.WithTimeout(context.Background(), channelConcurrencyGateOperationTimeout())
	defer cancel()
	redisNow, err := platformcache.RDB.Time(ctx).Result()
	if err != nil {
		reportChannelConcurrencyGateError(fmt.Errorf("read Redis time for channel lease snapshot: %w", err))
		return nil, false
	}
	minimumScore := "(" + strconv.FormatInt(redisNow.UnixMilli(), 10)
	pipe := platformcache.RDB.Pipeline()
	commands := make(map[int]*redis.IntCmd, len(uniqueIDs))
	for _, channelID := range uniqueIDs {
		keys := channelConcurrencyLeaseKeys(channelID, 0)
		commands[channelID] = pipe.ZCount(ctx, keys[0], minimumScore, "+inf")
	}
	if _, err = pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		reportChannelConcurrencyGateError(fmt.Errorf("load channel lease snapshot: %w", err))
		return nil, false
	}
	for channelID, command := range commands {
		count, commandErr := command.Result()
		if commandErr != nil && !errors.Is(commandErr, redis.Nil) {
			reportChannelConcurrencyGateError(fmt.Errorf("read channel %d lease count: %w", channelID, commandErr))
			return nil, false
		}
		result[channelID] = int(count)
	}
	return result, true
}

func reportChannelConcurrencyGateError(err error) {
	now := time.Now().UnixNano()
	last := channelConcurrencyGateLastError.Load()
	if last > 0 && time.Duration(now-last) < channelConcurrencyGateErrorInterval {
		return
	}
	if channelConcurrencyGateLastError.CompareAndSwap(last, now) {
		platformobservability.SysError("channel concurrency gate: " + err.Error())
	}
}
