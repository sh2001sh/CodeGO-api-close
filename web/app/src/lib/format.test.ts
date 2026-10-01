import { describe, expect, it } from 'vitest'
import { credits, toMicroCredits, shanghaiDay } from './format'
import { minorAmount, paymentAmount } from './commerce'

describe('exact monetary display and input', () => {
  it('selects the Shanghai calendar day across UTC midnight and year boundaries', () => {
    expect(shanghaiDay(new Date('2026-09-30T15:59:59Z'))).toBe('2026-09-30')
    expect(shanghaiDay(new Date('2026-09-30T16:00:00Z'))).toBe('2026-10-01')
    expect(shanghaiDay(new Date('2026-12-31T16:00:00Z'))).toBe('2027-01-01')
  })
  it('keeps six decimals and int64 boundaries exact', () => {
    expect(credits('1')).toBe('0.000001 credits')
    expect(credits('9223372036854775807')).toBe('9,223,372,036,854.775807 credits')
    expect(credits('-9223372036854775808')).toBe('-9,223,372,036,854.775808 credits')
    expect(toMicroCredits('1.000001')).toBe('1000001')
  })
  it('rejects ambiguous, negative and overflowing input', () => {
    for (const value of ['0', '-1', '1e3', '1.0000001', '9223372036854.775808', 'NaN'])
      expect(() => toMicroCredits(value)).toThrow()
    expect(minorAmount('12.34')).toBe(1234)
    expect(() => minorAmount('1.234')).toThrow()
    expect(() => minorAmount('90071992547410')).toThrow()
  })
  it('uses the configured currency smallest unit', () => {
    expect(minorAmount('123', 'jpy')).toBe(123)
    expect(() => minorAmount('1.1', 'jpy')).toThrow()
    expect(minorAmount('1.234', 'kwd')).toBe(1234)
    expect(paymentAmount('1234', 'kwd')).toBe('KWD 1.234')
    expect(paymentAmount('1234', 'jpy')).toBe('JPY 1,234')
  })
})
