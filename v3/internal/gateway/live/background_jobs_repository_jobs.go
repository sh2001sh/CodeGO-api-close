package live

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var backgroundCreateScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then return {-2} end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
redis.call('HSET', KEYS[1], 'id', ARGV[1], 'user', ARGV[2], 'key', ARGV[3],
  'data', ARGV[4], 'status', ARGV[5], 'billed', ARGV[6], 'cancel', ARGV[7],
  'lease', '', 'lease_until', 0, 'sequence', -1, 'updated', now)
if ARGV[6] == '1' then
  redis.call('PEXPIRE', KEYS[1], ARGV[9])
else
  redis.call('ZADD', KEYS[2], math.min(now, tonumber(ARGV[8])), ARGV[1])
end
return {1}
`)

func backgroundFlag(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func (r *RedisBackgroundRepository) Create(ctx context.Context, job BackgroundJob) error {
	if job.ID == "" || job.UserID <= 0 || job.KeyID <= 0 {
		return errors.New("live: background job identity is required")
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	job.UpdatedAt = job.CreatedAt
	job.LeaseID, job.LeaseUntil = "", time.Time{}
	sealed, err := r.seal(job.ID, "job", job)
	if err != nil {
		return err
	}
	key, _, pending := r.keys(job.ID)
	_, err = backgroundReply(backgroundCreateScript.Run(ctx, r.client, []string{key, pending}, job.ID, job.UserID, job.KeyID, sealed, job.Status, backgroundFlag(job.Billed), backgroundFlag(job.CancelRequested), job.CreatedAt.UnixMilli(), backgroundHistoryTTL.Milliseconds()).Result())
	return err
}

func (r *RedisBackgroundRepository) GetOwned(ctx context.Context, id string, userID, keyID int64) (BackgroundJob, error) {
	key, _, _ := r.keys(id)
	fields, err := r.client.HGetAll(ctx, key).Result()
	if err != nil {
		return BackgroundJob{}, err
	}
	if userID <= 0 || keyID <= 0 || fields["user"] != strconv.FormatInt(userID, 10) || fields["key"] != strconv.FormatInt(keyID, 10) {
		return BackgroundJob{}, ErrNotFound
	}
	return r.decode(id, fields)
}

func (r *RedisBackgroundRepository) Pending(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 {
		return []string{}, nil
	}
	_, _, pending := r.keys("")
	// Active leases are scheduled at their expiry, so leased/ambiguous jobs
	// cannot repeatedly occupy the first batch and starve newly queued work.
	return backgroundPendingScript.Run(ctx, r.client, []string{pending}, limit).StringSlice()
}

var backgroundPendingScript = redis.NewScript(`
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
return redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now, 'LIMIT', 0, ARGV[1])
`)

var backgroundClaimScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return {0} end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
local lease = redis.call('HGET', KEYS[1], 'lease')
local untilAt = tonumber(redis.call('HGET', KEYS[1], 'lease_until'))
if redis.call('HGET', KEYS[1], 'billed') == '1' or
   (untilAt > now and lease ~= ARGV[1]) then return {-1} end
redis.call('HSET', KEYS[1], 'lease', ARGV[1], 'lease_until', now + tonumber(ARGV[2]), 'updated', now)
redis.call('ZADD', KEYS[2], now + tonumber(ARGV[2]), ARGV[3])
return {1, unpack(redis.call('HGETALL', KEYS[1]))}
`)

func (r *RedisBackgroundRepository) Claim(ctx context.Context, id, lease string, ttl time.Duration) (BackgroundJob, error) {
	if lease == "" || ttl.Milliseconds() <= 0 {
		return BackgroundJob{}, errors.New("live: background lease and positive duration are required")
	}
	key, _, pending := r.keys(id)
	fields, err := backgroundReply(backgroundClaimScript.Run(ctx, r.client, []string{key, pending}, lease, ttl.Milliseconds(), id).Result())
	if err != nil {
		return BackgroundJob{}, err
	}
	return r.decode(id, fields)
}

var backgroundSaveScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return {0} end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
if redis.call('HGET', KEYS[1], 'lease') ~= ARGV[1] or
   tonumber(redis.call('HGET', KEYS[1], 'lease_until')) <= now or
   redis.call('HGET', KEYS[1], 'billed') == '1' then return {-1} end
if redis.call('HGET', KEYS[1], 'user') ~= ARGV[2] or
   redis.call('HGET', KEYS[1], 'key') ~= ARGV[3] then return {-1} end
redis.call('HSET', KEYS[1], 'data', ARGV[4], 'status', ARGV[5], 'billed', ARGV[6], 'updated', now)
if ARGV[7] == '1' then redis.call('HSET', KEYS[1], 'cancel', 1) end
if ARGV[6] == '1' then
  redis.call('ZREM', KEYS[3], ARGV[8])
  redis.call('HSET', KEYS[1], 'lease', '', 'lease_until', 0)
  redis.call('PEXPIRE', KEYS[1], ARGV[9])
  redis.call('PEXPIRE', KEYS[2], ARGV[9])
end
return {1}
`)

func (r *RedisBackgroundRepository) Save(ctx context.Context, job BackgroundJob) error {
	if job.LeaseID == "" {
		return ErrBackgroundLeaseConflict
	}
	sealed, err := r.seal(job.ID, "job", job)
	if err != nil {
		return err
	}
	key, events, pending := r.keys(job.ID)
	_, err = backgroundReply(backgroundSaveScript.Run(ctx, r.client, []string{key, events, pending}, job.LeaseID, job.UserID, job.KeyID, sealed, job.Status, backgroundFlag(job.Billed), backgroundFlag(job.CancelRequested), job.ID, backgroundHistoryTTL.Milliseconds()).Result())
	return err
}

var backgroundCancelScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 or
   redis.call('HGET', KEYS[1], 'user') ~= ARGV[1] or
   redis.call('HGET', KEYS[1], 'key') ~= ARGV[2] then return {0} end
if redis.call('HGET', KEYS[1], 'billed') == '1' then
  return {1, unpack(redis.call('HGETALL', KEYS[1]))}
end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
redis.call('HSET', KEYS[1], 'cancel', 1, 'updated', now)
return {1, unpack(redis.call('HGETALL', KEYS[1]))}
`)

func (r *RedisBackgroundRepository) Cancel(ctx context.Context, id string, userID, keyID int64) (BackgroundJob, error) {
	if userID <= 0 || keyID <= 0 {
		return BackgroundJob{}, ErrNotFound
	}
	key, _, _ := r.keys(id)
	fields, err := backgroundReply(backgroundCancelScript.Run(ctx, r.client, []string{key}, userID, keyID).Result())
	if err != nil {
		return BackgroundJob{}, err
	}
	return r.decode(id, fields)
}
