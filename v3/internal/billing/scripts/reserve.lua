-- Lua #1: reserve credits for one request (plan §3).
-- KEYS: 1 balance hash, 2 reservation hash, 3 done marker, 4 open-reservation zset, 5 account hold index
-- ARGV: 1 amount, 2 overdraft allowance, 3 expires (unix ms), 4 reservation ttl (ms), 5 zset member
-- Returns {code, amount, balance}: 1 reserved (or already reserved: idempotent),
-- -1 insufficient, -2 balance not loaded, -3 request already finalized.
-- balance is the account's current balance whenever the hash exists, so the
-- caller can keep a fresh snapshot for outage mode.
if redis.call('EXISTS', KEYS[3]) == 1 then
  return {-3, 0, 0}
end
if redis.call('EXISTS', KEYS[1]) == 0 then
  return {-2, 0, 0}
end
local fields = redis.call('HMGET', KEYS[1], 'balance', 'reserved')
local balance = fields[1] or '0'
local reserved = fields[2] or '0'
local existing = redis.call('HGET', KEYS[2], 'amount')
if existing then
  return {1, existing, balance}
end
if redis.call('HGET', KEYS[1], 'closed') == '1' then
  return {-1, 0, balance}
end
local amount = ARGV[1]
local total = moneyAdd(reserved, amount)
if not moneyFits(total) then return redis.error_reply('billing reserved overflow') end
if moneyCompare(moneyAdd(balance, ARGV[2]), total) < 0 then
  return {-1, 0, balance}
end
redis.call('HINCRBY', KEYS[1], 'reserved', amount)
redis.call('HSET', KEYS[2], 'amount', amount, 'expires', ARGV[3])
if ARGV[4] ~= '0' then redis.call('PEXPIRE', KEYS[2], ARGV[4]) end
redis.call('ZADD', KEYS[4], ARGV[3], ARGV[5])
redis.call('SADD', KEYS[5], KEYS[2])
return {1, amount, balance}
