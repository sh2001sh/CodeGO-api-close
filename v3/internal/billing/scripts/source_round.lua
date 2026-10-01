-- Round in the frozen source monetary unit, preserving oldquota * 2.
local function moneyScaleQuantum(a,b,denominator,quantum,rounding)
  return moneyMul(moneyMulDiv(a,b,moneyMul(denominator,quantum),rounding),quantum)
end
