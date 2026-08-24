import type { TFunction } from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { REDEMPTION_TYPES } from '../constants'
import { getRedemptionFormSchema } from './redemption-form'

const translate = ((key: string) => key) as TFunction

describe('redemption form schema', () => {
  test('normalizes missing numeric fields before business validation', () => {
    const result = getRedemptionFormSchema(translate).safeParse({
      name: '',
      redeem_type: REDEMPTION_TYPES.QUOTA,
    })

    assert.equal(result.success, false)
    if (!result.success) {
      assert.deepEqual(result.error.issues[0]?.path, ['quota_dollars'])
      assert.equal(
        result.error.issues[0]?.message,
        'Quota must be a positive number'
      )
      assert.equal(result.error.issues[0]?.code, 'custom')
    }
  })

  test('accepts subscription form values with inactive fields omitted', () => {
    const result = getRedemptionFormSchema(translate).parse({
      name: '',
      redeem_type: REDEMPTION_TYPES.SUBSCRIPTION,
      plan_id: 12,
    })

    assert.equal(result.quota_dollars, 0)
    assert.equal(result.blind_box_quantity, 1)
    assert.equal(result.count, 1)
  })

  test('accepts blind-box form values with inactive fields omitted', () => {
    const result = getRedemptionFormSchema(translate).parse({
      name: '',
      redeem_type: REDEMPTION_TYPES.BLIND_BOX,
      blind_box_quantity: 2,
    })

    assert.equal(result.quota_dollars, 0)
    assert.equal(result.plan_id, 0)
    assert.equal(result.count, 1)
  })
})
