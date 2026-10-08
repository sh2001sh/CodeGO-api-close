import type { Schema } from '../../lib/types'

export type Pool = Schema['ChannelMarketRoutePool']
export type PoolAutoBuild = Required<
  Pick<
    Schema['ChannelMarketAutoBuild'],
    | 'enabled'
    | 'models'
    | 'size'
    | 'explore'
    | 'schedule'
    | 'interval_minutes'
    | 'daily_time'
    | 'consumer_weight'
    | 'success_weight'
    | 'cache_weight'
  >
> &
  Pick<Schema['ChannelMarketAutoBuild'], 'last_build_at' | 'next_build_at' | 'last_error'>

export const defaultAutoBuild: PoolAutoBuild = {
  enabled: false,
  models: [],
  size: 3,
  explore: 0,
  schedule: 'interval',
  interval_minutes: 60,
  daily_time: '00:00',
  consumer_weight: 25,
  success_weight: 55,
  cache_weight: 20,
}

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('自动更新配置无效')
  return Object.fromEntries(Object.entries(value))
}

export function poolAutoBuild(pool: Pool | undefined): PoolAutoBuild {
  if (!pool) return { ...defaultAutoBuild, models: [] }
  const config = pool.config == null ? {} : record(pool.config)
  const raw = pool.auto_build ?? config.auto_build
  if (raw === undefined || raw === null) return { ...defaultAutoBuild, models: [] }
  const settings = record(raw)
  const number = (key: keyof PoolAutoBuild, fallback: number) => {
    const value = settings[key] ?? fallback
    if (typeof value !== 'number' || !Number.isFinite(value)) throw new Error('自动更新配置无效')
    return value
  }
  const string = (key: keyof PoolAutoBuild, fallback: string) => {
    const value = settings[key] ?? fallback
    if (typeof value !== 'string') throw new Error('自动更新配置无效')
    return value
  }
  if (settings.enabled !== undefined && typeof settings.enabled !== 'boolean')
    throw new Error('自动更新配置无效')
  const models = settings.models ?? []
  if (!Array.isArray(models) || models.some((value) => typeof value !== 'string'))
    throw new Error('自动更新配置无效')
  const schedule = string('schedule', 'interval')
  if (schedule !== 'interval' && schedule !== 'daily') throw new Error('自动更新配置无效')
  const result: PoolAutoBuild = {
    enabled: settings.enabled === true,
    models: models.map(String),
    schedule,
    size: number('size', 3),
    explore: number('explore', 0),
    interval_minutes: number('interval_minutes', 60),
    daily_time: string('daily_time', '00:00') || '00:00',
    consumer_weight: number('consumer_weight', 25),
    success_weight: number('success_weight', 55),
    cache_weight: number('cache_weight', 20),
    last_build_at: typeof settings.last_build_at === 'string' ? settings.last_build_at : undefined,
    next_build_at: typeof settings.next_build_at === 'string' ? settings.next_build_at : undefined,
    last_error: typeof settings.last_error === 'string' ? settings.last_error : undefined,
  }
  validateAutoBuild(result)
  return result
}

export function boundedInteger(value: string, minimum: number, maximum: number): number {
  if (!/^\d+$/.test(value)) throw new Error('请输入允许范围内的整数')
  const number = Number(value)
  if (!Number.isSafeInteger(number) || number < minimum || number > maximum)
    throw new Error('请输入允许范围内的整数')
  return number
}

export function validateAutoBuild(build: PoolAutoBuild) {
  boundedInteger(String(build.size), 1, 10)
  boundedInteger(String(build.explore), 0, build.size - 1)
  boundedInteger(String(build.interval_minutes), 1, 1440)
  if (build.schedule === 'daily' && !/^(?:[01]\d|2[0-3]):[0-5]\d$/.test(build.daily_time))
    throw new Error('请输入有效的 UTC 时间')
  for (const weight of [build.consumer_weight, build.success_weight, build.cache_weight])
    boundedInteger(String(weight), 0, 100)
}

export function poolStrategy(value: string): Pool['strategy'] {
  if (
    value === 'priority' ||
    value === 'cost' ||
    value === 'score' ||
    value === 'weighted' ||
    value === 'round_robin' ||
    value === 'fill_first'
  )
    return value
  throw new Error('请选择有效的路由策略')
}

/** Explicit order, independent of catalog order or numeric public ID. */
export function orderedMembers(ids: readonly string[]) {
  return [...new Set(ids)].map((group_id, priority) => ({ group_id, priority }))
}

export function moveMember(ids: readonly string[], index: number, delta: -1 | 1): string[] {
  const result = [...ids]
  const target = index + delta
  if (index < 0 || index >= ids.length || target < 0 || target >= ids.length) return result
  ;[result[index], result[target]] = [result[target], result[index]]
  return result
}

/** Canonical fields replace legacy config duplicates; server-owned build metadata is omitted. */
export function poolInput(
  pool: Pool | undefined,
  values: {
    name: string
    strategy: Pool['strategy']
    max_attempts: number
    failure_cooldown_seconds: number
    max_multiplier: number
    members: ReturnType<typeof orderedMembers>
    auto_build: PoolAutoBuild
  },
): Schema['ChannelMarketRoutePoolInput'] {
  const config = pool?.config == null ? {} : record(pool.config)
  const {
    last_build_at: _last,
    next_build_at: _next,
    last_error: _error,
    ...auto_build
  } = values.auto_build
  return {
    id: pool?.id ?? '',
    ...values,
    auto_build,
    config: {
      ...config,
      strategy: values.strategy,
      max_attempts: values.max_attempts,
      failure_cooldown_seconds: values.failure_cooldown_seconds,
      max_multiplier: values.max_multiplier,
      auto_build,
    },
  }
}
