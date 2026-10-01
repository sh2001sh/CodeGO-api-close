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
