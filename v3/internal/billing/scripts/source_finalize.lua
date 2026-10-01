-- source-v1,wallet,subscription,cap,ttl,request,count,budgetIndex,
-- walletBefore,subscriptionBefore,cardID,grossDivisor,quantum;
-- account,held,eligible,modelKey,modelLimit,modelUsed,subscriptionID tuples;
-- common event pairs. All preflights precede all writes.
local function finalizeSource()
  if redis.call('EXISTS',KEYS[1]) == 1 then return {0,0,0,redis.call('GET',KEYS[1])} end
  local count,budgetIndex=tonumber(ARGV[7]),tonumber(ARGV[8])
  local walletIndex=budgetIndex > 0 and count-1 or count
  local wallet,subscription,remaining=ARGV[2],ARGV[3],ARGV[3]
  local quantum=ARGV[13]
  local preference='subscription_first'
  for j=14+count*7,#ARGV,2 do if ARGV[j] == 'funding_preference' then preference=ARGV[j+1] end end
  local charges,helds,loaded={},{},{}
  local available,allowed={},{}
  local modelKeys,modelUsed,modelHeld,modelReserved={},{},{},{}
  local subTotal,actual='0','0'
  for i=1,walletIndex-1 do
    local base=4+(i-1)*4
    modelKeys[i]=ARGV[10+i*7]
    modelUsed[i]=redis.call('HGET',KEYS[base],modelKeys[i] .. ':used') or ARGV[12+i*7]
    modelReserved[i]=redis.call('HGET',KEYS[base],modelKeys[i] .. ':reserved') or '0'
    modelHeld[i]=redis.call('HGET',KEYS[base+1],'model_amount') or '0'
    local admitted=ARGV[8+i*7]
    local liveHeld=redis.call('HGET',KEYS[base+1],'amount') or '0'
    if moneyCompare(admitted,liveHeld) > 0 then admitted=liveHeld end
    available[i]=admitted;allowed[i]=ARGV[9+i*7]
    if preference == 'subscription_only' and allowed[i] == '2' and redis.call('EXISTS',KEYS[base+1]) == 1 and redis.call('HGET',KEYS[base],'closed') ~= '1' then
      available[i]=moneyAdd(moneyAdd(redis.call('HGET',KEYS[base],'balance') or '0',liveHeld),moneyNegate(redis.call('HGET',KEYS[base],'reserved') or '0'))
      if moneyCompare(available[i],'0') < 0 then available[i]='0' end
    end
    if allowed[i] ~= '0' then
      local limit=ARGV[11+i*7]
      if limit ~= '0' then
        local capAvailable=moneyAdd(moneyAdd(limit,moneyNegate(modelUsed[i])),moneyAdd(modelHeld[i],moneyNegate(modelReserved[i])))
        if moneyCompare(capAvailable,'0') < 0 then capAvailable='0' end
        if moneyCompare(available[i],capAvailable) > 0 then available[i]=capAvailable end
      end
    end
  end
  local walletBase=4+(walletIndex-1)*4
  available[walletIndex]=redis.call('HGET',KEYS[walletBase+1],'amount') or '0'
  local affordable,walletCharge
  charges,subTotal,walletCharge,affordable=allocateSource(wallet,subscription,quantum,preference,available,allowed,walletIndex,true)
  charges[walletIndex]=walletCharge
  -- Delivered service must settle accepted subscription money even if the
  -- actual usage exceeds available funds. Preserve source-only preference and
  -- retain the exact uncharged amount in every ledger event for investigation.
  local shortfall='0'
  if not affordable then shortfall=moneyAdd(subscription,moneyNegate(subTotal)) end
  remaining=moneyAdd(subscription,moneyNegate(subTotal))
  actual=moneyAdd(subTotal,charges[walletIndex])
  if not moneyFits(actual) then return redis.error_reply('billing source aggregate overflow') end
  if budgetIndex > 0 then charges[budgetIndex]=actual end
  local gross=moneyScaleQuantum(moneyAdd(subTotal,moneyMul(charges[walletIndex],ARGV[12])),'1',ARGV[12],quantum,'half_up')
  local before=ARGV[9]
  if subscription ~= '0' then
    if preference == 'subscription_only' then
      before=moneyScaleQuantum(subTotal,ARGV[10],subscription,quantum,'half_up')
    else
      local weighted=moneyAdd(moneyMul(subTotal,ARGV[10]),moneyMul(remaining,ARGV[9]))
      before=moneyScaleQuantum(weighted,'1',subscription,quantum,'half_up')
    end
  elseif preference == 'subscription_only' then
    before=ARGV[10]
  end
  -- Conservative residual wallet rounding may add one micro to actual.
  if moneyCompare(before,actual) < 0 then before=actual end
  if not moneyFits(gross) or not moneyFits(before) then return redis.error_reply('billing source audit overflow') end
  for i=1,count do
    local base=4+(i-1)*4
    loaded[i]=redis.call('EXISTS',KEYS[base]) == 1
    helds[i]=redis.call('HGET',KEYS[base+1],'amount') or '0'
    if loaded[i] then
      local balance=redis.call('HGET',KEYS[base],'balance') or '0'
      if not moneyFits(moneyAdd(balance,moneyNegate(charges[i]))) then return redis.error_reply('billing balance overflow') end
      if moneyCompare(redis.call('HGET',KEYS[base],'reserved') or '0',helds[i]) < 0 then return redis.error_reply('billing hold exceeds reserved') end
      if charges[i] ~= '0' and redis.call('HGET',KEYS[base],'ver') == '9223372036854775807' then return redis.error_reply('billing version overflow') end
      if i < walletIndex then
        if moneyCompare(modelReserved[i],modelHeld[i]) < 0 then return redis.error_reply('billing model hold exceeds reserved') end
        if not moneyFits(moneyAdd(modelUsed[i],charges[i])) then return redis.error_reply('billing model usage overflow') end
      end
    end
  end
  local source='wallet'
  if preference == 'subscription_only' then source='subscription' end
  if subTotal ~= '0' then source=charges[walletIndex] ~= '0' and 'mixed' or 'subscription' end
  local after,overdraft,logged='0',0,false
  for i=1,count do
    local base,account=4+(i-1)*4,ARGV[7+i*7]
    local balance,flag='0',0
    if loaded[i] then
      if helds[i] ~= '0' then redis.call('HINCRBY',KEYS[base],'reserved',moneyNegate(helds[i])) end
      if charges[i] ~= '0' then
        redis.call('HINCRBY',KEYS[base],'balance',moneyNegate(charges[i]))
        redis.call('HINCRBY',KEYS[base],'ver',1)
      end
      if i < walletIndex and modelKeys[i] ~= '' then
        redis.call('HSETNX',KEYS[base],modelKeys[i] .. ':used',modelUsed[i])
        if modelHeld[i] ~= '0' then redis.call('HINCRBY',KEYS[base],modelKeys[i] .. ':reserved',moneyNegate(modelHeld[i])) end
        if charges[i] ~= '0' then redis.call('HINCRBY',KEYS[base],modelKeys[i] .. ':used',charges[i]) end
      end
      balance=redis.call('HGET',KEYS[base],'balance') or '0'
      if moneyCompare(balance,moneyNegate(ARGV[4])) < 0 then flag=1 end
    end
    redis.call('DEL',KEYS[base+1])
    redis.call('SREM',KEYS[base+3],KEYS[base+1])
    redis.call('ZREM',KEYS[2],account .. ':' .. ARGV[6])
    if ARGV[5] == '0' then redis.call('SET',KEYS[base+2],actual) else redis.call('SET',KEYS[base+2],actual,'PX',ARGV[5]) end
    if charges[i] ~= '0' or (actual == '0' and i == walletIndex) then
      local event={}
      for j=14+count*7,#ARGV,2 do
        local name,value=ARGV[j],ARGV[j+1]
        if name ~= 'prop_id' and name ~= 'before_micro' and name ~= 'after_micro' then
          if name == 'account_id' then value=account end
          if name == 'amount' then value=charges[i] end
          if name == 'billing_source' then value=source end
          if name == 'marketplace_gross_micro' then value=gross end
          event[#event+1]=name;event[#event+1]=value
        end
      end
      if not logged and ARGV[11] ~= '0' and moneyCompare(before,actual) > 0 then
        event[#event+1]='prop_id';event[#event+1]=ARGV[11]
        event[#event+1]='before_micro';event[#event+1]=before
        event[#event+1]='after_micro';event[#event+1]=actual
      end
      event[#event+1]='billing_source';event[#event+1]=source
      event[#event+1]='reserved';event[#event+1]=helds[i]
      event[#event+1]='overdraft';event[#event+1]=tostring(flag)
      event[#event+1]='balance_loaded';event[#event+1]=loaded[i] and '1' or '0'
      event[#event+1]='funding_part';event[#event+1]=logged and 'secondary' or 'primary'
      event[#event+1]='usage_total_amount';event[#event+1]=actual
      if shortfall ~= '0' then
        event[#event+1]='source_shortfall_micro';event[#event+1]=shortfall
        event[#event+1]='source_requested_micro';event[#event+1]=subscription
      end
      if i < walletIndex and ARGV[13+i*7] ~= '0' then
        event[#event+1]='subscription_id';event[#event+1]=ARGV[13+i*7]
        event[#event+1]='subscription_model_debit';event[#event+1]=charges[i]
      end
      redis.call('XADD',KEYS[3],'*',unpack(event));logged=true
    end
    if i == walletIndex then after=balance;overdraft=flag end
  end
  if ARGV[5] == '0' then redis.call('SET',KEYS[1],actual) else redis.call('SET',KEYS[1],actual,'PX',ARGV[5]) end
  return {1,after,overdraft,actual,shortfall}
end
