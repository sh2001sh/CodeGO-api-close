import { describe, expect, it } from 'vitest'
import { factor, integer } from './form'

function fields(name: string, value: string) {
  const data = new FormData()
  data.set(name, value)
  return data
}

describe('渠道市场操作输入', () => {
  it('保持完整的用户和 Key ID 精度', () => {
    expect(integer(fields('id', '9223372036854775807'), 'id')).toBe(9223372036854775807n)
    expect(() => integer(fields('id', '9223372036854775808'), 'id')).toThrow('ID 超出允许范围')
  })
  it('拒绝零、负数和非整数 ID', () => {
    for (const invalid of ['0', '-1', '1.5', '1e3', '']) {
      expect(() => integer(fields('id', invalid), 'id')).toThrow('请输入有效的正整数 ID')
    }
  })
  it('限制倍率范围与小数精度', () => {
    expect(factor(fields('multiplier', '0.000001'))).toBe(0.000001)
    expect(factor(fields('multiplier', '1000'))).toBe(1000)
    for (const invalid of ['0', '-1', '1000.000001', '0.1234567', 'NaN']) {
      expect(() => factor(fields('multiplier', invalid))).toThrow()
    }
  })
})
