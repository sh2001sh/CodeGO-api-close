-- KEYS: balance,reservation,done,global open index,account holds,business index.
-- ARGV: amount,PG balance,PG version,transaction ID,operation ID,check time,member.
if redis.call('EXISTS',KEYS[3]) == 1 then return -3 end
if redis.call('HEXISTS',KEYS[1],'balance') == 0 then
  redis.call('HSET',KEYS[1],'balance',ARGV[2],'reserved','0','ver',ARGV[3],'base',ARGV[3])
end
local existing=redis.call('HGET',KEYS[2],'amount')
if existing then
  if redis.call('HGET',KEYS[2],'business_xid') == ARGV[4] and existing == ARGV[1] then return 1 end
  return -4
end
local fields=redis.call('HMGET',KEYS[1],'balance','reserved')
local total=moneyAdd(fields[2] or '0',ARGV[1])
if not moneyFits(total) then return redis.error_reply('billing reserved overflow') end
if moneyCompare(fields[1],total) < 0 then return -1 end
redis.call('HINCRBY',KEYS[1],'reserved',ARGV[1])
redis.call('HSET',KEYS[2],'amount',ARGV[1],'expires',ARGV[6],'business_xid',ARGV[4],'operation_id',ARGV[5])
-- No TTL: a PG transaction still in progress must never lose its budget hold.
redis.call('SADD',KEYS[5],KEYS[2])
redis.call('ZADD',KEYS[4],ARGV[6],ARGV[7])
redis.call('ZADD',KEYS[6],ARGV[6],ARGV[7])
return 1
