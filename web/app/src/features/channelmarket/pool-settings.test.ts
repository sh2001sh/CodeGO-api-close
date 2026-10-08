import { describe, expect, it } from 'vitest'
import {
  defaultAutoBuild,
  moveMember,
  orderedMembers,
  poolInput,
  validateAutoBuild,
  poolAutoBuild,
  type Pool,
} from './pool-settings'

const pool: Pool = {
  id: 'pool-1',
  owner_user_id: '9223372036854775807',
  name: '自选',
  strategy: 'priority',
  max_attempts: 3,
  failure_cooldown_seconds: 30,
  max_multiplier: 0,
  members: [
    { group_id: 'b', priority: 0 },
    { group_id: 'a', priority: 1 },
  ],
  config: {
    strategy: 'cost',
    max_attempts: 1,
    auto_build: {
      ...defaultAutoBuild,
      enabled: true,
      models: ['gpt-4o'],
      last_build_at: '2026-10-01T00:00:00Z',
    },
  },
}
describe('route pool editor contract', () => {
  it('keeps a user-selected order rather than catalog checkbox order', () => {
    const order = moveMember(['b', 'a', 'c'], 1, -1)
    expect(order).toEqual(['a', 'b', 'c'])
    expect(orderedMembers([...order, 'b'])).toEqual([
      { group_id: 'a', priority: 0 },
      { group_id: 'b', priority: 1 },
      { group_id: 'c', priority: 2 },
    ])
    expect(moveMember(order, 0, -1)).toEqual(order)
  })
  it('removes client-supplied build metadata and replaces stale legacy routing copies', () => {
    const build = poolAutoBuild(pool)
    const input = poolInput(pool, {
      name: '新名',
      strategy: 'round_robin',
      max_attempts: 7,
      failure_cooldown_seconds: 90,
      max_multiplier: 0.8,
      members: orderedMembers(['a', 'b']),
      auto_build: { ...build, enabled: false },
    })
    expect(input.auto_build).toMatchObject({ enabled: false, models: ['gpt-4o'] })
    expect(input.auto_build).not.toHaveProperty('last_build_at')
    expect(input.config).toMatchObject({
      strategy: 'round_robin',
      max_attempts: 7,
      failure_cooldown_seconds: 90,
      max_multiplier: 0.8,
    })
    expect(pool.owner_user_id).toBe('9223372036854775807')
  })
  it('rejects range, daily UTC and exploration boundaries instead of sending invalid settings', () => {
    for (const change of [
      { size: 0 },
      { size: 11 },
      { explore: 3 },
      { interval_minutes: 1441 },
      { schedule: 'daily' as const, daily_time: '24:00' },
      { success_weight: 100.1 },
      { cache_weight: 1.5 },
    ])
      expect(() => validateAutoBuild({ ...defaultAutoBuild, ...change })).toThrow()
    expect(() =>
      validateAutoBuild({
        ...defaultAutoBuild,
        size: 1,
        explore: 0,
        daily_time: '23:59',
        interval_minutes: 1440,
      }),
    ).not.toThrow()
    expect(() =>
      poolAutoBuild({
        ...pool,
        auto_build: undefined,
        config: { auto_build: { ...defaultAutoBuild, schedule: 'unknown' } },
      }),
    ).toThrow('自动更新配置无效')
  })
  it('loads persisted interval plans with an empty daily clock without disabling editing', () => {
    const persisted = poolAutoBuild({
      ...pool,
      auto_build: { ...defaultAutoBuild, daily_time: '' },
    })
    expect(persisted.daily_time).toBe('00:00')
    expect(() => validateAutoBuild({ ...defaultAutoBuild, daily_time: '' })).not.toThrow()
    expect(() =>
      validateAutoBuild({ ...defaultAutoBuild, schedule: 'daily', daily_time: '' }),
    ).toThrow()
  })
})
