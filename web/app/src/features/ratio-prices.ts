import type { Schema } from '../lib/types'

type Rational = { n: bigint; d: bigint }
function decimal(value: unknown): Rational {
  if (typeof value !== 'number' && typeof value !== 'string') throw new Error('上游价格格式无效')
  const text = String(value)
  if (!/^\d+(\.\d{1,18})?$/.test(text)) throw new Error('上游价格格式无效')
  const [whole, fraction = ''] = text.split('.')
  return { n: BigInt(whole + fraction), d: 10n ** BigInt(fraction.length) }
}
function rounded(value: Rational): string {
  const result = (value.n * 2n + value.d) / (value.d * 2n)
  if (result > 9223372036854775807n) throw new Error('价格超出允许范围')
  return String(result)
}
function multiply(a: Rational, b: Rational): Rational {
  return { n: a.n * b.n, d: a.d * b.d }
}

export function ratioPrice(
  model: string,
  differences: Record<string, Schema['RatioSyncDifference']>,
  upstream: string,
  current?: Schema['CatalogPrice'],
): Schema['CatalogPrice'] {
  if (current && !['per_token', 'per_request'].includes(current.mode))
    throw new Error('该模型包含特殊计费规则，请在价格编辑器中完整核对后同步')
  const read = (field: string) => {
    const item = differences[field]
    const value = item?.upstreams[upstream]
    if (value === 'same' || value === null || value === undefined) return item?.current
    if (item?.confidence[upstream] === false)
      throw new Error('此来源的倍率可信度不足，请核对后手动修改价格')
    return value
  }
  for (const field of ['image_ratio', 'audio_ratio', 'audio_completion_ratio', 'billing_expr']) {
    const value = differences[field]?.upstreams[upstream]
    if (value !== null && value !== undefined && value !== 'same')
      throw new Error('该模型包含特殊计费规则，请在价格编辑器中完整核对后同步')
  }
  const price = read('model_price')
  const modelRatio = read('model_ratio')
  if (price !== undefined && price !== null)
    return {
      model,
      mode: 'per_request',
      per_request: rounded(multiply(decimal(price), { n: 1000000n, d: 1n })),
      input_per_mtok: 0,
      output_per_mtok: 0,
      cache_read_per_mtok: 0,
      cache_write_per_mtok: 0,
      rules: current?.rules ?? {},
    }
  const input =
    modelRatio !== undefined && modelRatio !== null
      ? multiply(decimal(modelRatio), { n: 2000000n, d: 1n })
      : current
        ? { n: BigInt(current.input_per_mtok), d: 1n }
        : null
  if (!input) throw new Error('上游未提供输入价格，无法自动同步')
  const amount = (field: string, fallback: number | string | bigint | undefined) => {
    const value = read(field)
    if (value !== undefined && value !== null) return rounded(multiply(input, decimal(value)))
    if (fallback !== undefined && current && BigInt(current.input_per_mtok) > 0n)
      return rounded(multiply(input, { n: BigInt(fallback), d: BigInt(current.input_per_mtok) }))
    if (fallback !== undefined && BigInt(fallback) === 0n) return '0'
    throw new Error('上游价格不完整，请补齐输出和缓存价格')
  }
  return {
    model,
    mode: 'per_token',
    input_per_mtok: rounded(input),
    output_per_mtok: amount('completion_ratio', current?.output_per_mtok),
    cache_read_per_mtok: amount('cache_ratio', current?.cache_read_per_mtok),
    cache_write_per_mtok: amount('create_cache_ratio', current?.cache_write_per_mtok),
    per_request: 0,
    rules: current?.rules ?? {},
  }
}
