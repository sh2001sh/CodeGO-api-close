-- KEYS use the reserve layout. ARGV: funding,actual,cap,doneTTL,request,count;
-- then account,reserved pairs; then common event field/value pairs.
-- WAL replay uses precisely this script; all source balances and events settle
-- atomically. Actual beyond the estimate is charged to the final wallet.
if ARGV[1] == 'source-v1' then return finalizeSource() end
if redis.call('EXISTS', KEYS[1]) == 1 then return {0,0,0} end
local count = tonumber(ARGV[6])
local actual=ARGV[2]
local remaining,charges = actual,{}
local budgetIndex = 0
for j=7+count*2,#ARGV,2 do
  if ARGV[j] == 'budget_account_id' and ARGV[j+1] == ARGV[5+count*2] then budgetIndex=count end
end
local walletIndex = budgetIndex > 0 and count-1 or count
for i=1,count do
  local held = ARGV[6 + i*2]
  if i == budgetIndex then charges[i]=actual
  else
    charges[i] = i == walletIndex and remaining or (moneyCompare(remaining, held) <= 0 and remaining or held)
    remaining = moneyAdd(remaining,moneyNegate(charges[i]))
  end
  local base = 4 + (i-1)*4
  if redis.call('EXISTS',KEYS[base]) == 1 then
    local balance = redis.call('HGET',KEYS[base],'balance') or '0'
    if not moneyFits(moneyAdd(balance,moneyNegate(charges[i]))) then return redis.error_reply('billing balance overflow') end
    local held=redis.call('HGET',KEYS[base+1],'amount') or '0'
    if held ~= '0' and moneyCompare(redis.call('HGET',KEYS[base],'reserved') or '0',held) < 0 then return redis.error_reply('billing hold exceeds reserved') end
    if charges[i] ~= '0' and redis.call('HGET',KEYS[base],'ver') == '9223372036854775807' then return redis.error_reply('billing version overflow') end
  end
end
local subscriptionCharge='0'
for i=1,walletIndex-1 do subscriptionCharge=moneyAdd(subscriptionCharge,charges[i]) end
local billingSource='wallet'
if subscriptionCharge ~= '0' then billingSource=charges[walletIndex] ~= '0' and 'mixed' or 'subscription' end
local after,overdraft,logged = '0',0,false
for i=1,count do
  local base,account = 4+(i-1)*4, ARGV[5+i*2]
  local held = redis.call('HGET',KEYS[base+1],'amount') or '0'
  local loaded = redis.call('EXISTS',KEYS[base]) == 1
  local balance,flag = '0',0
  if loaded then
    if held ~= '0' then redis.call('HINCRBY',KEYS[base],'reserved',moneyNegate(held)) end
    if charges[i] ~= '0' then
      redis.call('HINCRBY',KEYS[base],'balance',moneyNegate(charges[i]))
      redis.call('HINCRBY',KEYS[base],'ver',1)
    end
    balance = redis.call('HGET',KEYS[base],'balance') or '0'
    if moneyCompare(balance,moneyNegate(ARGV[3])) < 0 then flag = 1 end
  end
  redis.call('DEL',KEYS[base+1])
  redis.call('SREM',KEYS[base+3],KEYS[base+1])
  redis.call('ZREM',KEYS[2],account .. ':' .. ARGV[5])
  if ARGV[4] == '0' then redis.call('SET',KEYS[base+2],actual) else redis.call('SET',KEYS[base+2],actual,'PX',ARGV[4]) end
  if charges[i] ~= '0' or (actual == '0' and i == walletIndex) then
    local event = {}
    for j=7+count*2,#ARGV,2 do
      local name,value=ARGV[j],ARGV[j+1]
      if name == 'account_id' then value=account end
      if name == 'amount' then value=charges[i] end
      if name == 'billing_source' then value=billingSource end
      event[#event+1]=name;event[#event+1]=value
    end
    if logged then
      -- Only the primary event owns a card's aggregate discount audit.
      local cleaned={}
      for j=1,#event,2 do
        if event[j] ~= 'prop_id' and event[j] ~= 'before_micro' and event[j] ~= 'after_micro' then
          cleaned[#cleaned+1]=event[j];cleaned[#cleaned+1]=event[j+1]
        end
      end
      event=cleaned
    end
    event[#event+1]='reserved';event[#event+1]=held
    event[#event+1]='overdraft';event[#event+1]=tostring(flag)
    event[#event+1]='balance_loaded';event[#event+1]=loaded and '1' or '0'
    event[#event+1]='funding_part';event[#event+1]=logged and 'secondary' or 'primary'
    event[#event+1]='usage_total_amount';event[#event+1]=actual
    redis.call('XADD',KEYS[3],'*',unpack(event))
    logged=true
  end
  if i == walletIndex then after=balance;overdraft=flag end
end
if ARGV[4] == '0' then redis.call('SET',KEYS[1],actual) else redis.call('SET',KEYS[1],actual,'PX',ARGV[4]) end
return {1,after,overdraft}
