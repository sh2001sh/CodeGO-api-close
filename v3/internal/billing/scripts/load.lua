-- Install an account's ledger balance into Redis, only if it is not there yet.
-- Concurrent first requests for one account all call this; the first wins and
-- the rest are no-ops, so a load can never overwrite live holds or charges.
-- KEYS: 1 balance hash
-- ARGV: 1 balance (micro-credits), 2 ledger version
-- Returns 1 installed, 0 already present.
if redis.call('EXISTS', KEYS[1]) == 1 then
  return 0
end
-- 'base' remembers the ledger version this hash started from, so the
-- reconciler can tell whether Redis has applied any charge since the load.
redis.call('HSET', KEYS[1], 'balance', ARGV[1], 'reserved', 0, 'ver', ARGV[2], 'base', ARGV[2])
return 1
