import type { APIKey, Schema } from '../../lib/types'

export type RestrictionMode = 'all' | 'selected' | 'none'

export function restrictionMode(values: string[] | null | undefined): RestrictionMode {
  if (values == null) return 'all'
  return values.length ? 'selected' : 'none'
}

export function scaledDecimal(value: number | string | bigint | null | undefined): string {
  const amount = BigInt(value ?? 0)
  const sign = amount < 0n ? '-' : ''
  const absolute = amount < 0n ? -amount : amount
  const fraction = String(absolute % 1_000_000n)
    .padStart(6, '0')
    .replace(/0+$/, '')
  return `${sign}${absolute / 1_000_000n}${fraction ? `.${fraction}` : ''}`
}

function parseScaled(value: string): string {
  if (!/^\d+(?:\.\d{1,6})?$/.test(value)) throw new Error('请输入最多六位小数的非负数')
  const [whole, fraction = ''] = value.split('.')
  const amount = BigInt(whole) * 1_000_000n + BigInt(fraction.padEnd(6, '0'))
  if (amount > 9_223_372_036_854_775_807n) throw new Error('金额超出允许范围')
  return String(amount)
}

function restriction(fields: FormData, name: string, max: number): string[] | null {
  const mode = fields.get(`${name}_mode`)
  if (mode === 'all') return null
  if (mode === 'none') return []
  if (mode !== 'selected') throw new Error('请检查访问限制')
  const values = [
    ...new Set(
      String(fields.get(name) ?? '')
        .split(/[,\n]/)
        .map((v) => v.trim())
        .filter(Boolean),
    ),
  ]
  if (!values.length || values.length > max) throw new Error('请检查访问限制')
  return values
}

export function keyInput(fields: FormData, previous?: APIKey): Schema['KeyInput'] {
  const expiration = String(fields.get('expires_at') ?? '')
  if (expiration && Number.isNaN(new Date(expiration).getTime())) throw new Error('到期时间无效')
  const limited = fields.get('budget_limited') === 'on'
  const enteredBudget = String(fields.get('budget') ?? '')
  const unchangedBudget =
    previous?.budget_limited && enteredBudget === scaledDecimal(previous.budget_micro_credits)
  const budget = limited && !unchangedBudget ? parseScaled(enteredBudget) : undefined
  const result: Schema['KeyInput'] = {
    id: previous?.id ?? 0,
    name: String(fields.get('name') ?? '').trim(),
    status: String(fields.get('status') ?? previous?.status ?? 'active'),
    group: String(fields.get('group') ?? '') || null,
    allowed_models: restriction(fields, 'models', 1000),
    allowed_cidrs: restriction(fields, 'cidrs', 100),
    expires_at: expiration ? new Date(expiration).toISOString() : null,
    budget_limited: limited,
    cross_group_retry: fields.get('cross_group_retry') === 'on',
    max_marketplace_multiplier_ppm:
      fields.get('multiplier_unlimited') === 'on'
        ? '0'
        : parseScaled(String(fields.get('max_multiplier') ?? '1')),
  }
  if (result.max_marketplace_multiplier_ppm === '0' && fields.get('multiplier_unlimited') !== 'on')
    throw new Error('倍率必须大于零')
  // Leaving the displayed allowance unchanged must not overwrite concurrent spending.
  if (
    budget !== undefined &&
    (!previous?.budget_limited || budget !== String(previous.budget_micro_credits ?? 0))
  )
    result.budget_micro_credits = budget
  if (!result.name || result.name.length > 100) throw new Error('请检查名称')
  return result
}

export function keyStatusInput(
  key: APIKey,
  status: 'active' | 'disabled',
): Schema['KeyWriteInput'] {
  return { id: key.id, status }
}
