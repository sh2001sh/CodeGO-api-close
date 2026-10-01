-- Release one reservation whose gateway died (or is still streaming) past expiry.
-- KEYS: 1 balance hash, 2 reservation hash, 3 done marker, 4 open zset, 5 event stream
-- ARGV: 1 now (unix ms), 2 zset member, 3 request id, 4 account id
-- Returns 1 released, 0 nothing to do, -1 reservation hash lost (amount unknown).
--
-- The sweep only releases the hold. It does NOT set the done marker: if the
-- request is merely slow and finalizes later, that finalize must still charge
-- (it finds no hold and charges the full actual amount).
-- Business cleanup is fenced by PG transaction ID: an old sweeper must never
-- remove a callback retry's newer reservation with the same operation ID.
if ARGV[5] and (redis.call('HGET',KEYS[2],'business_xid') or '') ~= ARGV[5] then return 0 end
if redis.call('EXISTS', KEYS[3]) == 1 then
  redis.call('ZREM', KEYS[4], ARGV[2])
  if KEYS[7] then redis.call('ZREM',KEYS[7],ARGV[2]) end
  return 0
end
local fields = redis.call('HMGET', KEYS[2], 'amount', 'expires')
if not fields[1] then
  -- Only possible if the hash TTL fired before the sweeper ran; reserve sets
  -- that TTL well past the expiry to prevent it. The caller logs this.
  redis.call('ZREM', KEYS[4], ARGV[2])
  if KEYS[6] then redis.call('SREM', KEYS[6], KEYS[2]) end
  if KEYS[7] then redis.call('ZREM',KEYS[7],ARGV[2]) end
  return -1
end
if tonumber(fields[2]) > tonumber(ARGV[1]) then
  return 0
end
local held = fields[1]
if held ~= '0' and redis.call('EXISTS', KEYS[1]) == 1 then
  redis.call('HINCRBY', KEYS[1], 'reserved', moneyNegate(held))
end
local modelKey=redis.call('HGET',KEYS[2],'model_key')
local modelHeld=redis.call('HGET',KEYS[2],'model_amount') or '0'
if modelKey and modelHeld ~= '0' and redis.call('EXISTS',KEYS[1]) == 1 then
  redis.call('HINCRBY',KEYS[1],modelKey .. ':reserved',moneyNegate(modelHeld))
end
redis.call('DEL', KEYS[2])
redis.call('ZREM', KEYS[4], ARGV[2])
if KEYS[6] then redis.call('SREM', KEYS[6], KEYS[2]) end
if KEYS[7] then redis.call('ZREM',KEYS[7],ARGV[2]) end
-- Business rollback is not gateway usage and must not create a usage log.
if ARGV[5] then return 1 end
redis.call('XADD', KEYS[5], '*', 'request_id', ARGV[3], 'account_id', ARGV[4],
  'amount', '0', 'reserved', tostring(held), 'terminal', 'reservation_expired',
  'overdraft', '0', 'balance_loaded', '1', 'ts', ARGV[1])
return 1
