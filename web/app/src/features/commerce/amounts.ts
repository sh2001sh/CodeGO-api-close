import { toMicroCredits } from '../../lib/format'
export { paymentAmount as invoicePayment } from '../../lib/commerce'

export function transferAmounts(value: string, feeBPS: number) {
  if (!Number.isInteger(feeBPS) || feeBPS < 0 || feeBPS > 10000)
    throw new Error('服务器手续费配置无效')
  const amount = BigInt(toMicroCredits(value))
  const fee = (amount * BigInt(feeBPS) + 9999n) / 10000n
  const total = amount + fee
  if (total > 9223372036854775807n) throw new Error('含手续费金额超出允许范围')
  return { amount, fee, total }
}

export function nonnegativeMicroCredits(value: string): bigint {
  if (/^0(?:\.0{1,6})?$/.test(value)) return 0n
  return BigInt(toMicroCredits(value))
}

export function decimalCredits(value: number | string | bigint): string {
  const amount = BigInt(value)
  return `${amount / 1_000_000n}.${String(amount % 1_000_000n).padStart(6, '0')}`
}

export function positiveID(value: string): string {
  if (!/^\d+$/.test(value) || BigInt(value) <= 0n || BigInt(value) > 9223372036854775807n)
    throw new Error('请输入有效的正整数编号')
  return BigInt(value).toString()
}

export function steppedMicroCredits(
  value: string,
  minimum: number | string | bigint,
  step: number | string | bigint,
): bigint {
  const amount = BigInt(toMicroCredits(value))
  const min = BigInt(minimum)
  const increment = BigInt(step)
  if (min <= 0n || increment <= 0n) throw new Error('服务器额度配置无效')
  if (amount < min) throw new Error('额度低于套餐最低购买量')
  if (amount % increment !== 0n) throw new Error('额度不符合套餐购买步长')
  return amount
}

export function errorFrom(cause: unknown) {
  return cause instanceof Error ? cause : new Error('输入无效')
}
