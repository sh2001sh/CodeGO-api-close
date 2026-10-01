-- Integer decimal helpers: Lua doubles cannot represent all bigint amounts.
-- Comparisons and admission arithmetic must retain exact micro-credit digits.
local function magnitude(x)
  local negative = string.sub(x, 1, 1) == '-'
  if negative then x = string.sub(x, 2) end
  x = string.gsub(x, '^0+', '')
  if x == '' then return '0', false end
  return x, negative
end
local function cmpMagnitude(a, b)
  if #a ~= #b then return #a < #b and -1 or 1 end
  if a == b then return 0 end
  return a < b and -1 or 1
end
local function moneyCompare(a, b)
  local am, an = magnitude(a)
  local bm, bn = magnitude(b)
  if an ~= bn then return an and -1 or 1 end
  local c = cmpMagnitude(am, bm)
  return an and -c or c
end
local function moneyNegate(a)
  local m, n = magnitude(a)
  if m == '0' then return m end
  return n and m or '-' .. m
end
local function addMagnitude(a, b)
  local result, carry, i, j = '', 0, #a, #b
  while i > 0 or j > 0 or carry > 0 do
    local digit = carry
    if i > 0 then digit = digit + tonumber(string.sub(a, i, i)); i = i - 1 end
    if j > 0 then digit = digit + tonumber(string.sub(b, j, j)); j = j - 1 end
    result = tostring(digit % 10) .. result
    carry = math.floor(digit / 10)
  end
  return result
end
local function subMagnitude(a, b)
  local result, borrow, i, j = '', 0, #a, #b
  while i > 0 do
    local digit = tonumber(string.sub(a, i, i)) - borrow
    if j > 0 then digit = digit - tonumber(string.sub(b, j, j)); j = j - 1 end
    if digit < 0 then digit = digit + 10; borrow = 1 else borrow = 0 end
    result = tostring(digit) .. result
    i = i - 1
  end
  return magnitude(result)
end
local function moneyAdd(a, b)
  local am, an = magnitude(a)
  local bm, bn = magnitude(b)
  local m, n
  if an == bn then m, n = addMagnitude(am, bm), an
  elseif cmpMagnitude(am, bm) >= 0 then m, n = subMagnitude(am, bm), an
  else m, n = subMagnitude(bm, am), bn end
  if m == '0' then return m end
  return n and '-' .. m or m
end
local function moneyFits(a)
  return moneyCompare(a, '-9223372036854775808') >= 0 and moneyCompare(a, '9223372036854775807') <= 0
end

