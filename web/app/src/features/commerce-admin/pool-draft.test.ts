import { describe, expect, it } from 'vitest'
import { newStandardPolicy, parsePoolConfig, poolConfigJSON } from './pool-draft'

const reward = {
  kind: 'credits',
  title: 'Range',
  weight: 9223372036854775807n,
  amount_micro: 0,
  minimum_micro: 100,
  maximum_micro: 1000,
  step_micro: 5,
  multiplier_ppm: 0,
  duration_seconds: 0,
  plan_id: 0,
  wallet_type: 'bonus',
  reward_tier: 'rare',
  legacy_reward_type: 'credits',
}

describe('full blind-box pool draft', () => {
  it('roundtrips exact int64 weights, credit ranges, coupon caps, guarantees and standard policy', () => {
    const guarantees = {
      first: [{ ...reward, weight: 1 }],
      small_after: 4,
      small_reset_micro: 100,
      small: [{ ...reward, weight: 1 }],
    }
    const policy = {
      ...newStandardPolicy,
      enabled: true,
      subscription_probability_ppb: 50,
      subscription_plan_id: 9007199254740993n,
    }
    const parsed = parsePoolConfig(
      poolConfigJSON([reward]),
      poolConfigJSON(guarantees),
      poolConfigJSON(policy),
      'standard',
    )
    expect(parsed.rewards).toEqual([reward])
    expect(parsed.guarantees).toEqual(guarantees)
    expect(parsed.standard_policy).toEqual(policy)
    const coupon = {
      ...reward,
      kind: 'topup_discount',
      weight: 1,
      amount_micro: 0,
      discount_rate_ppm: 900000,
      max_discount_micro: 50000000,
      prop_type: 'topup_discount_90',
    }
    expect(
      parsePoolConfig(poolConfigJSON([coupon]), '{}', poolConfigJSON(newStandardPolicy), 'credits')
        .rewards,
    ).toEqual([coupon])
  })

  it('rejects broken JSON and incomplete policy rather than clearing existing fields', () => {
    expect(() => parsePoolConfig('[', '{}', '{}', 'credits')).toThrow('JSON')
    expect(() => parsePoolConfig(poolConfigJSON([reward]), '{}', '{}', 'credits')).toThrow(
      '标准池策略',
    )
    expect(() => parsePoolConfig('{}', '{}', poolConfigJSON(newStandardPolicy), 'credits')).toThrow(
      '奖励配置',
    )
    expect(() => parsePoolConfig('[]', '{}', poolConfigJSON(newStandardPolicy), 'credits')).toThrow(
      '奖励配置',
    )
    expect(() =>
      parsePoolConfig(poolConfigJSON([reward]), '[]', poolConfigJSON(newStandardPolicy), 'credits'),
    ).toThrow('保底配置')
  })

  it('rejects invalid reward ranges, missing guarantee rewards, overflowing weights and incompatible scope', () => {
    for (const invalid of [
      { ...reward, minimum_micro: 2000 },
      { ...reward, weight: -1 },
      { ...reward, kind: 'unknown' },
    ]) {
      expect(() =>
        parsePoolConfig(
          poolConfigJSON([invalid]),
          '{}',
          poolConfigJSON(newStandardPolicy),
          'credits',
        ),
      ).toThrow('奖励配置')
    }
    expect(() =>
      parsePoolConfig(
        poolConfigJSON([reward, { ...reward, weight: 1 }]),
        '{}',
        poolConfigJSON(newStandardPolicy),
        'credits',
      ),
    ).toThrow('奖励配置')
    expect(() =>
      parsePoolConfig(
        poolConfigJSON([reward]),
        '{"small_after":5,"small_reset_micro":100}',
        poolConfigJSON(newStandardPolicy),
        'credits',
      ),
    ).toThrow('保底配置')
    expect(() =>
      parsePoolConfig(
        poolConfigJSON([reward]),
        '{}',
        poolConfigJSON({ ...newStandardPolicy, enabled: true }),
        'credits',
      ),
    ).toThrow('标准池策略')
  })
})
