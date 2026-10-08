import { describe, expect, it } from 'vitest'
import { interpolateTranslation } from './i18n'
import { minorAmount } from './commerce'
import english from '../locales/en'
import hk from '../locales/zh-HK.json'
import ar from '../locales/ar.json'

describe('translated parameter templates', () => {
  const dictionaries: Record<string, Record<string, string>> = { en: english, hk, ar }
  for (const [locale, dictionary] of Object.entries(dictionaries)) {
    const t = (key: string, parameters?: Record<string, string | number>) =>
      interpolateTranslation(dictionary[key] ?? key, parameters)
    it(`${locale} substitutes key counts and payment precision after translation`, () => {
      const key = '将删除 {count} 个 Key，删除后无法恢复，确认继续？'
      expect(t(key, { count: 2 })).toBe(dictionary[key].replace('{count}', '2'))
      expect(() => minorAmount('1.234', 'usd', t)).toThrow(
        dictionary['支付金额最多支持 {digits} 位小数'].replace('{digits}', '2'),
      )
      expect(() => minorAmount('1.1', 'jpy', t)).toThrow(
        dictionary['支付金额最多支持 {digits} 位小数'].replace('{digits}', '0'),
      )
    })
  }
  it('keeps parameter text literal and does not recursively interpolate it', () => {
    expect(
      interpolateTranslation('{name}: {count}; {missing}', { name: '{count}$&', count: 0 }),
    ).toBe('{count}$&: 0; {missing}')
  })
})
