-- Funding ratios may multiply bigint balances into 57-digit intermediates.
-- Only single decimal digits enter Lua arithmetic; callers enforce int64
-- bounds on the final result before changing any balance.
local function sourceMagnitude(value)
  if type(value) ~= 'string' or not string.match(value, '^%d+$') then
    error('source money must be a nonnegative decimal integer')
  end
  return (magnitude(value))
end

local function moneyMul(a, b)
  a, b = sourceMagnitude(a), sourceMagnitude(b)
  if a == '0' or b == '0' then return '0' end
  local digits = {}
  for i = 1, #a + #b do digits[i] = 0 end
  for i = #a, 1, -1 do
    local carry, left = 0, string.byte(a, i) - 48
    for j = #b, 1, -1 do
      local index = i + j
      local product = left * (string.byte(b, j) - 48) + digits[index] + carry
      digits[index], carry = product % 10, math.floor(product / 10)
    end
    digits[i] = carry
  end
  for i = 1, #digits do digits[i] = tostring(digits[i]) end
  return (magnitude(table.concat(digits)))
end

local function moneyDivmod(a, b)
  a, b = sourceMagnitude(a), sourceMagnitude(b)
  if b == '0' then error('source money division by zero') end
  local multiples = {'0'}
  for digit = 1, 9 do multiples[digit + 1] = addMagnitude(multiples[digit], b) end
  local quotient, remainder = {}, '0'
  for i = 1, #a do
    remainder = magnitude(remainder .. string.sub(a, i, i))
    local digit = 9
    while cmpMagnitude(remainder, multiples[digit + 1]) < 0 do digit = digit - 1 end
    quotient[i] = tostring(digit)
    remainder = subMagnitude(remainder, multiples[digit + 1])
  end
  return magnitude(table.concat(quotient)), remainder
end

local function moneyMulDiv(a, b, denominator, rounding)
  if rounding ~= 'floor' and rounding ~= 'ceil' and rounding ~= 'half_up' then
    error('unknown source money rounding')
  end
  denominator = sourceMagnitude(denominator)
  local quotient, remainder = moneyDivmod(moneyMul(a, b), denominator)
  if remainder ~= '0' and (rounding == 'ceil' or
      (rounding == 'half_up' and cmpMagnitude(addMagnitude(remainder, remainder), denominator) >= 0)) then
    quotient = addMagnitude(quotient, '1')
  end
  return quotient
end
