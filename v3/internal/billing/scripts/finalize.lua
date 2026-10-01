-- Lua #3: settle or release a reservation and emit its billing event (plan §3, §5).
-- The balance change and the event are one atomic step: the ledger worker can
-- never see a charge that did not happen, or miss one that did.
-- KEYS: 1 balance hash, 2 reservation hash, 3 done marker, 4 open zset, 5 event stream
-- ARGV: 1 actual charge (0 = release), 2 overdraft cap, 3 done ttl (ms), 4 zset member,
--       5.. event field/value pairs
-- Returns {code, balance_after, overdraft}: 1 applied, 0 already finalized.
if redis.call('EXISTS', KEYS[3]) == 1 then
  return {0, 0, 0}
end
local actual = ARGV[1]
local held = redis.call('HGET', KEYS[2], 'amount') or '0'

local loaded = redis.call('EXISTS', KEYS[1]) == 1
local after = 0
local overdraft = 0
if loaded then
  local current = redis.call('HGET', KEYS[1], 'balance') or '0'
  if not moneyFits(moneyAdd(current, moneyNegate(actual))) then return redis.error_reply('billing balance overflow') end
  if held ~= '0' and moneyCompare(redis.call('HGET',KEYS[1],'reserved') or '0',held) < 0 then return redis.error_reply('billing hold exceeds reserved') end
  if actual ~= '0' and redis.call('HGET',KEYS[1],'ver') == '9223372036854775807' then return redis.error_reply('billing version overflow') end
  if held ~= '0' then
    redis.call('HINCRBY', KEYS[1], 'reserved', moneyNegate(held))
  end
  if actual ~= '0' then
    redis.call('HINCRBY', KEYS[1], 'balance', moneyNegate(actual))
    after = redis.call('HGET', KEYS[1], 'balance')
    redis.call('HINCRBY', KEYS[1], 'ver', 1)
  else
    after = redis.call('HGET', KEYS[1], 'balance') or '0'
  end
  if actual ~= '0' and moneyCompare(after, moneyNegate(ARGV[2])) < 0 then
    overdraft = 1 -- charged anyway: the upstream already did the work
  end
end

redis.call('DEL', KEYS[2])
redis.call('ZREM', KEYS[4], ARGV[4])
if KEYS[6] then redis.call('SREM', KEYS[6], KEYS[2]) end
if ARGV[3] == '0' then redis.call('SET',KEYS[3],actual) else redis.call('SET', KEYS[3], actual, 'PX', ARGV[3]) end

local event = {}
for i = 5, #ARGV do
  event[#event+1]=ARGV[i]
end
event[#event + 1] = 'reserved'
event[#event + 1] = held
event[#event + 1] = 'overdraft'
event[#event + 1] = tostring(overdraft)
event[#event + 1] = 'balance_loaded'
event[#event + 1] = loaded and '1' or '0'
redis.call('XADD', KEYS[5], '*', unpack(event))
return {1, after, overdraft}
