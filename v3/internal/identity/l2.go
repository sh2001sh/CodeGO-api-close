package identity

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

// l2 is the shared Redis profile cache. Every write is conditional on the
// invalidation tombstones, and every invalidation writes a tombstone, so a
// PostgreSQL read that raced an invalidation can never repopulate stale data.
//
// The scripts touch keys of one user and one key id together; this assumes a
// single Redis (or a cluster with all identity keys pinned to one slot).
type l2 struct {
	rdb  *redisx.Client
	ttl  time.Duration
	tomb time.Duration // must exceed ttl plus the longest possible load
}

func l2HashKey(hash [32]byte) string { return redisx.KeyAPIKeyPrefix + hex.EncodeToString(hash[:]) }
func l2UserSet(userID int64) string {
	return redisx.KeyAPIKeyUserPrefix + strconv.FormatInt(userID, 10)
}
func l2IDKey(keyID int64) string { return redisx.KeyAPIKeyIDPrefix + strconv.FormatInt(keyID, 10) }
func l2UserTomb(userID int64) string {
	return redisx.KeyAPIKeyTombPrefix + "user:" + strconv.FormatInt(userID, 10)
}
func l2KeyTomb(keyID int64) string {
	return redisx.KeyAPIKeyTombPrefix + "key:" + strconv.FormatInt(keyID, 10)
}

func (c *l2) get(ctx context.Context, hash [32]byte) (*KeyProfile, bool, error) {
	blob, err := c.rdb.Get(ctx, l2HashKey(hash)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var p KeyProfile
	if err := json.Unmarshal(blob, &p); err != nil {
		return nil, false, err
	}
	return &p, true, nil
}

// clock returns Redis server time in ms, the single clock all tombstones use.
func (c *l2) clock(ctx context.Context) (int64, error) {
	t, err := c.rdb.Time(ctx).Result()
	if err != nil {
		return 0, err
	}
	return t.UnixMilli(), nil
}

// put stores p unless the key or its user was invalidated at or after
// loadStart. It reports whether the entry was written.
var putScript = redis.NewScript(`
local loadStart = tonumber(ARGV[1])
for i = 4, 5 do
  local t = redis.call('GET', KEYS[i])
  if t and tonumber(t) >= loadStart then return 0 end
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
redis.call('SADD', KEYS[2], ARGV[4])
redis.call('PEXPIRE', KEYS[2], ARGV[3])
redis.call('SET', KEYS[3], ARGV[4], 'PX', ARGV[3])
return 1
`)

func (c *l2) put(ctx context.Context, hash [32]byte, p *KeyProfile, loadStart int64) (bool, error) {
	blob, err := json.Marshal(p)
	if err != nil {
		return false, err
	}
	keys := []string{l2HashKey(hash), l2UserSet(p.UserID), l2IDKey(p.KeyID), l2KeyTomb(p.KeyID), l2UserTomb(p.UserID)}
	n, err := putScript.Run(ctx, c.rdb, keys, loadStart, blob, jitter(c.ttl).Milliseconds(), hex.EncodeToString(hash[:])).Int()
	return n == 1, err
}

// invalidateKey drops one key's L2 entry and fences in-flight loads.
var invalidateKeyScript = redis.NewScript(`
local t = redis.call('TIME')
redis.call('SET', KEYS[2], t[1] * 1000 + math.floor(t[2] / 1000), 'PX', ARGV[1])
local h = redis.call('GET', KEYS[1])
if h then redis.call('DEL', ARGV[2] .. h) end
redis.call('DEL', KEYS[1])
return 1
`)

func (c *l2) invalidateKey(ctx context.Context, keyID int64) error {
	keys := []string{l2IDKey(keyID), l2KeyTomb(keyID)}
	return invalidateKeyScript.Run(ctx, c.rdb, keys, c.tomb.Milliseconds(), redisx.KeyAPIKeyPrefix).Err()
}

// invalidateUser drops every L2 entry of a user and fences in-flight loads.
var invalidateUserScript = redis.NewScript(`
local t = redis.call('TIME')
redis.call('SET', KEYS[2], t[1] * 1000 + math.floor(t[2] / 1000), 'PX', ARGV[1])
for _, h in ipairs(redis.call('SMEMBERS', KEYS[1])) do
  redis.call('DEL', ARGV[2] .. h)
end
redis.call('DEL', KEYS[1])
return 1
`)

func (c *l2) invalidateUser(ctx context.Context, userID int64) error {
	keys := []string{l2UserSet(userID), l2UserTomb(userID)}
	return invalidateUserScript.Run(ctx, c.rdb, keys, c.tomb.Milliseconds(), redisx.KeyAPIKeyPrefix).Err()
}
