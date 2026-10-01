package billing

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

var recomputeReservedScript = redis.NewScript(`
-- KEYS: balance hash, account hold index. All reservation mutation and this
-- reconstruction run atomically on the same Redis primary. HINCRBY preserves
-- full int64 precision; summing Lua numbers would lose credits above 2^53.
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
local old = redis.call('HGET', KEYS[1], 'reserved') or '0'
redis.call('HSET', KEYS[1], 'recomputing', '0')
for _, key in ipairs(redis.call('SMEMBERS', KEYS[2])) do
  local amount = redis.call('HGET', key, 'amount')
  if amount then
    redis.call('HINCRBY', KEYS[1], 'recomputing', amount)
  else
    redis.call('SREM', KEYS[2], key)
  end
end
local total = redis.call('HGET', KEYS[1], 'recomputing')
redis.call('HDEL', KEYS[1], 'recomputing')
redis.call('HSET', KEYS[1], 'reserved', total)
return old == total and 0 or 1
`)

// RecomputeReserved repairs holds lost to reservation TTL expiry without
// touching balance/version or racing a reserve/finalize operation.
func RecomputeReserved(ctx context.Context, rdb *redisx.Client, accountID int64) (bool, error) {
	n, err := recomputeReservedScript.Run(ctx, rdb, []string{BalanceKey(accountID), ReservationIndexKey(accountID)}).Int()
	if err != nil {
		return false, fmt.Errorf("billing: reconstruct reserved %d: %w", accountID, err)
	}
	return n == 1, nil
}
