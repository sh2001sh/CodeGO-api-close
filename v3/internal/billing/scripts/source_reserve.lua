-- Source prices describe the same service in wallet and subscription money.
-- Every candidate must be affordable; hold each account's largest scenario.
local function reserveSource()
  if redis.call('EXISTS',KEYS[1]) == 1 then return {-3} end
  local count,candidates=(#KEYS-3)/4,tonumber(ARGV[6])
  local budgetIndex=tonumber(ARGV[7])
  local walletIndex=budgetIndex > 0 and count-1 or count
  local available,amounts,reserved,existingCount={},{},{},0
  local modelKeys,modelUsed,modelReserved={},{},{}
  for i=1,count do
    local base=4+(i-1)*4
    if redis.call('EXISTS',KEYS[base]) == 0 then return {-2,i} end
    reserved[i]=redis.call('HGET',KEYS[base],'reserved') or '0'
    local existing=redis.call('HGET',KEYS[base+1],'amount')
    if existing then existingCount=existingCount+1;amounts[i]=existing else amounts[i]='0' end
    available[i]=moneyAdd(redis.call('HGET',KEYS[base],'balance') or '0',moneyNegate(reserved[i]))
    if redis.call('HGET',KEYS[base],'closed') == '1' then available[i]='0'
    elseif i == walletIndex then available[i]=moneyAdd(available[i],ARGV[2]) end
    if moneyCompare(available[i],'0') < 0 then available[i]='0' end
    local cap=8+count+(i-1)*3
    modelKeys[i]=ARGV[cap]
    modelUsed[i]=redis.call('HGET',KEYS[base],modelKeys[i] .. ':used') or ARGV[cap+2]
    modelReserved[i]=redis.call('HGET',KEYS[base],modelKeys[i] .. ':reserved') or '0'
    if i < walletIndex and ARGV[cap+1] ~= '0' then
      local remaining=moneyAdd(moneyAdd(ARGV[cap+1],moneyNegate(modelUsed[i])),moneyNegate(modelReserved[i]))
      if moneyCompare(remaining,'0') < 0 then remaining='0' end
      if moneyCompare(remaining,available[i]) < 0 then available[i]=remaining end
    end
  end
  if existingCount == count then
    local result={1}
    for i=1,count do result[#result+1]=amounts[i] end
    result[#result+1]=redis.call('HGET',KEYS[5],'source_total') or '0'
    return result
  end
  if existingCount ~= 0 then return redis.error_reply('billing partial source reservation') end
  local maximum='0'
  local preference=ARGV[8+count*4]
  for candidate=1,candidates do
    local start=9+count*4+(candidate-1)*(count+3)
    local wallet,subscription,quantum=ARGV[start],ARGV[start+1],ARGV[start+2]
    local allowed={}
    for i=1,walletIndex-1 do allowed[i]=ARGV[start+2+i] end
    local scenario,subTotal,walletCharge,affordable=allocateSource(wallet,subscription,quantum,preference,available,allowed,walletIndex,false)
    if not affordable then return {-1} end
    local total=moneyAdd(subTotal,walletCharge)
    if not moneyFits(total) then return redis.error_reply('billing source aggregate overflow') end
    if budgetIndex > 0 then
      if moneyCompare(total,available[budgetIndex]) > 0 then return {-1} end
      scenario[budgetIndex]=total
    end
    for i=1,count do
      if moneyCompare(scenario[i],amounts[i]) > 0 then amounts[i]=scenario[i] end
    end
    if moneyCompare(total,maximum) > 0 then maximum=total end
  end
  for i=1,count do
    if not moneyFits(moneyAdd(reserved[i],amounts[i])) then return redis.error_reply('billing reserved overflow') end
    if i < walletIndex and not moneyFits(moneyAdd(modelReserved[i],amounts[i])) then return redis.error_reply('billing model reserved overflow') end
  end
  local result={1}
  for i=1,count do
    local base=4+(i-1)*4
    redis.call('HINCRBY',KEYS[base],'reserved',amounts[i])
    redis.call('HSET',KEYS[base+1],'amount',amounts[i],'expires',ARGV[3],'source_total',maximum)
    if i < walletIndex and modelKeys[i] ~= '' then
      redis.call('HSETNX',KEYS[base],modelKeys[i] .. ':used',modelUsed[i])
      redis.call('HINCRBY',KEYS[base],modelKeys[i] .. ':reserved',amounts[i])
      redis.call('HSET',KEYS[base+1],'model_key',modelKeys[i],'model_amount',amounts[i])
    end
    if ARGV[4] ~= '0' then redis.call('PEXPIRE',KEYS[base+1],ARGV[4]) end
    redis.call('SADD',KEYS[base+3],KEYS[base+1])
    redis.call('ZADD',KEYS[2],ARGV[3],ARGV[7+i] .. ':' .. ARGV[5])
    result[#result+1]=amounts[i]
  end
  result[#result+1]=maximum
  return result
end
