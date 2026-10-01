package live

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
)

var backgroundAppendScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return {0} end
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
if redis.call('HGET', KEYS[1], 'lease') ~= ARGV[1] or
   tonumber(redis.call('HGET', KEYS[1], 'lease_until')) <= now or
   redis.call('HGET', KEYS[1], 'billed') == '1' then return {-1} end
local sequence = redis.call('HINCRBY', KEYS[1], 'sequence', 1)
redis.call('RPUSH', KEYS[2], ARGV[2])
return {1, sequence}
`)

func (r *RedisBackgroundRepository) Append(ctx context.Context, id, lease string, event BackgroundEvent) (int64, error) {
	if lease == "" {
		return 0, ErrBackgroundLeaseConflict
	}
	event.Sequence = 0 // The Redis append, not the caller, assigns the sequence.
	sealed, err := r.seal(id, "event", event)
	if err != nil {
		return 0, err
	}
	key, events, _ := r.keys(id)
	result, err := backgroundAppendScript.Run(ctx, r.client, []string{key, events}, lease, sealed).Result()
	if err != nil {
		return 0, err
	}
	values, ok := result.([]any)
	if !ok || len(values) == 0 {
		return 0, errors.New("live: invalid background event reply")
	}
	code, ok := values[0].(int64)
	if !ok {
		return 0, errors.New("live: invalid background event result")
	}
	if code == 0 {
		return 0, ErrNotFound
	}
	if code == -1 {
		return 0, ErrBackgroundLeaseConflict
	}
	if code != 1 || len(values) != 2 {
		return 0, errors.New("live: invalid background event result")
	}
	sequence, ok := values[1].(int64)
	if !ok {
		return 0, errors.New("live: invalid background event sequence")
	}
	return sequence, nil
}

var backgroundEventsScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return {0} end
return {1, unpack(redis.call('LRANGE', KEYS[2], ARGV[1], ARGV[2]))}
`)

func (r *RedisBackgroundRepository) Events(ctx context.Context, id string, after int64, limit int) ([]BackgroundEvent, error) {
	if after < -1 {
		return nil, errors.New("live: background event cursor must be at least -1")
	}
	if limit <= 0 || after == int64(^uint64(0)>>1) {
		return []BackgroundEvent{}, nil
	}
	// Bound the Lua reply below its unpack stack limit; callers resume from the
	// last returned sequence to retrieve the remaining events.
	limit = min(limit, 1024)
	start := after + 1
	end := start + int64(limit) - 1
	if end < start {
		end = int64(^uint64(0) >> 1)
	}
	key, events, _ := r.keys(id)
	result, err := backgroundEventsScript.Run(ctx, r.client, []string{key, events}, start, end).Result()
	if err != nil {
		return nil, err
	}
	values, ok := result.([]any)
	if !ok || len(values) == 0 {
		return nil, errors.New("live: invalid background events reply")
	}
	if values[0] == int64(0) {
		return nil, ErrNotFound
	}
	if values[0] != int64(1) {
		return nil, errors.New("live: invalid background events result")
	}
	output := make([]BackgroundEvent, 0, len(values)-1)
	for i, value := range values[1:] {
		sealed, ok := value.(string)
		if !ok {
			return nil, errors.New("live: invalid encrypted background event")
		}
		var event BackgroundEvent
		if err := r.open(id, "event", []byte(sealed), &event); err != nil {
			return nil, err
		}
		event.Sequence = start + int64(i)
		output = append(output, event)
	}
	return output, nil
}
