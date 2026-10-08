-- Both prices describe a whole service; allocation preserves its paid share.
-- Admission requires affordable final wallet money. Settlement preserves held
-- subscription limits and charges unreserved service only to an allowed wallet.
local function allocateSource(wallet,subscription,quantum,preference,available,allowed,walletIndex,settlement)
  local charges,subTotal={},'0'
  for i=1,walletIndex do charges[i]='0' end
  if preference == 'release' then return charges,'0','0',true end
  if preference == 'wallet_only' then
    charges[walletIndex]=wallet
    return charges,'0',wallet,settlement or moneyCompare(wallet,available[walletIndex]) <= 0
  end
  local anyAllowed=false
  for i=1,walletIndex-1 do anyAllowed=anyAllowed or allowed[i] ~= '0' end
  if preference == 'subscription_only' and not anyAllowed then return charges,'0','0',false end
  local remaining=subscription
  if preference == 'wallet_first' then
    if wallet == '0' then return charges,'0','0',true end
    charges[walletIndex]=moneyCompare(wallet,available[walletIndex]) <= 0 and wallet or available[walletIndex]
    charges[walletIndex]=moneyScaleQuantum(charges[walletIndex],'1','1',quantum,'floor')
    local residual=moneyAdd(wallet,moneyNegate(charges[walletIndex]))
    if residual == '0' then return charges,'0',charges[walletIndex],true end
    if subscription == '0' then
      if not settlement then return charges,'0',charges[walletIndex],false end
      charges[walletIndex]=wallet;return charges,'0',wallet,true
    end
    remaining=moneyScaleQuantum(subscription,residual,wallet,quantum,'ceil')
  end
  for i=1,walletIndex-1 do
    if allowed[i] ~= '0' then
      charges[i]=moneyCompare(remaining,available[i]) <= 0 and remaining or available[i]
      charges[i]=moneyScaleQuantum(charges[i],'1','1',quantum,'floor')
      remaining=moneyAdd(remaining,moneyNegate(charges[i]))
      subTotal=moneyAdd(subTotal,charges[i])
    end
  end
  if preference == 'subscription_only' then return charges,subTotal,'0',remaining == '0' end
  if preference == 'wallet_first' then
    if remaining ~= '0' then
      if not settlement then return charges,subTotal,charges[walletIndex],false end
      charges[walletIndex]=moneyAdd(charges[walletIndex],moneyScaleQuantum(wallet,remaining,subscription,quantum,'ceil'))
    end
  else
    charges[walletIndex]=subscription == '0' and wallet or moneyScaleQuantum(wallet,remaining,subscription,quantum,'ceil')
  end
  local affordable=settlement or moneyCompare(charges[walletIndex],available[walletIndex]) <= 0
  return charges,subTotal,charges[walletIndex],affordable
end

-- V2 buckets may price the same service in different units. A common exact
-- service denominator is walletQuote * legacyQuote (at most 38 digits); every
-- bucket quote is one of these two values. No floating point shares enter.
local function allocateBuckets(wallet,legacy,prices,quantum,preference,available,allowed,walletIndex,settlement)
  local charges,shares,subTotal={},{},'0'
  for i=1,walletIndex do charges[i]='0';shares[i]='0' end
  local unit=moneyMul(wallet == '0' and '1' or wallet,legacy == '0' and '1' or legacy)
  local remaining=unit
  if preference == 'release' then return charges,'0','0',true,'0',unit,shares end
  if preference == 'wallet_only' or preference == 'wallet_first' then
    if wallet == '0' then shares[walletIndex]=unit;return charges,'0','0',true,'0',unit,shares end
    local charge=wallet
    if preference == 'wallet_first' and moneyCompare(charge,available[walletIndex])>0 then charge=available[walletIndex] end
    charge=moneyScaleQuantum(charge,'1','1',quantum,'floor')
    charges[walletIndex]=charge
    shares[walletIndex]=moneyMulDiv(charge,unit,wallet,'floor')
    remaining=moneyAdd(remaining,moneyNegate(shares[walletIndex]))
    if preference == 'wallet_only' then return charges,'0',charge,settlement or moneyCompare(charge,available[walletIndex])<=0,remaining,unit,shares end
  end
  local anyAllowed=false
  for i=1,walletIndex-1 do
    if allowed[i] ~= '0' then
      anyAllowed=true
      if remaining ~= '0' then
        if prices[i] == '0' then shares[i]=remaining;remaining='0'
        else
          local wanted=moneyScaleQuantum(prices[i],remaining,unit,quantum,'ceil')
          local charge=moneyCompare(wanted,available[i])<=0 and wanted or available[i]
          charge=moneyScaleQuantum(charge,'1','1',quantum,'floor')
          charges[i]=charge;subTotal=moneyAdd(subTotal,charge)
          local share=moneyMulDiv(charge,unit,prices[i],'floor')
          if moneyCompare(share,remaining)>0 then share=remaining end
          shares[i]=share
          remaining=moneyAdd(remaining,moneyNegate(share))
        end
      end
    end
  end
  if preference == 'subscription_only' then return charges,subTotal,'0',anyAllowed and remaining=='0',remaining,unit,shares end
  local residual=moneyScaleQuantum(wallet,remaining,unit,quantum,'ceil')
  if preference == 'wallet_first' and residual ~= '0' and not settlement then return charges,subTotal,charges[walletIndex],false,remaining,unit,shares end
  charges[walletIndex]=moneyAdd(charges[walletIndex],residual)
  shares[walletIndex]=moneyAdd(shares[walletIndex],remaining)
  return charges,subTotal,charges[walletIndex],settlement or moneyCompare(charges[walletIndex],available[walletIndex])<=0,remaining,unit,shares
end
