-- KEYS: request done, open index, events; then per source balance,reservation,done,hold-index.
-- Sources are ordered subscriptions then wallet. ARGV: amount,allowance,expires,ttl,request,account IDs.
-- Admission and every source hold are atomic; insufficient total funds never
-- leave a partial subscription reservation behind.
if ARGV[1] == 'source-v1' then return reserveSource() end
if redis.call('EXISTS', KEYS[1]) == 1 then return {-3} end
local count = (#KEYS - 3) / 4
local budgetIndex = tonumber(ARGV[6+count] or '0')
local walletIndex = budgetIndex > 0 and count-1 or count
local amounts, existingCount, remaining = {}, 0, ARGV[1]
for i = 1, count do
  local base = 4 + (i - 1) * 4
  if redis.call('EXISTS', KEYS[base]) == 0 then return {-2, i} end
  local existing = redis.call('HGET', KEYS[base+1], 'amount')
  if existing then existingCount = existingCount + 1; amounts[i] = existing end
end
if existingCount == count then
  local result = {1}
  for i=1,count do result[#result+1]=amounts[i] end
  return result
end
if existingCount ~= 0 then return redis.error_reply('billing partial funding reservation') end
for i = 1, count do
  local base = 4 + (i - 1) * 4
  local fields = redis.call('HMGET', KEYS[base], 'balance', 'reserved')
  local available = moneyAdd(fields[1] or '0', moneyNegate(fields[2] or '0'))
  if redis.call('HGET',KEYS[base],'closed') == '1' then
    available='0'
  elseif i == walletIndex then
    available = moneyAdd(available, ARGV[2])
  end
  if moneyCompare(available, '0') < 0 then available = '0' end
  if i == budgetIndex then
    if moneyCompare(available,ARGV[1]) < 0 then return {-1} end
    amounts[i] = ARGV[1]
  else
    amounts[i] = moneyCompare(remaining, available) <= 0 and remaining or available
    remaining = moneyAdd(remaining, moneyNegate(amounts[i]))
  end
  if not moneyFits(moneyAdd(fields[2] or '0', amounts[i])) then return redis.error_reply('billing reserved overflow') end
end
if remaining ~= '0' then return {-1} end
local result = {1}
for i = 1, count do
  local base = 4 + (i - 1) * 4
  redis.call('HINCRBY', KEYS[base], 'reserved', amounts[i])
  redis.call('HSET', KEYS[base+1], 'amount', amounts[i], 'expires', ARGV[3])
  if ARGV[4] ~= '0' then redis.call('PEXPIRE', KEYS[base+1], ARGV[4]) end
  redis.call('SADD', KEYS[base+3], KEYS[base+1])
  redis.call('ZADD', KEYS[2], ARGV[3], ARGV[5+i] .. ':' .. ARGV[5])
  result[#result+1] = amounts[i]
end
return result
