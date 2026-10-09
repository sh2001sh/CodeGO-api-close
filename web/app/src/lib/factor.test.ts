import { describe, expect, it } from 'vitest'
import { factorText, ppmFactorText } from './factor'

describe('exact marketplace factor display', () => {
  it('scales decimal PPM without rounding long or tiny historical factors', () => {
    expect(ppmFactorText('131145.14191981')).toBe('0.13114514191981')
    expect(ppmFactorText('1e-57')).toBe(`0.${'0'.repeat(62)}1`)
    expect(factorText('1e-63')).toBe(`0.${'0'.repeat(62)}1`)
    expect(ppmFactorText('0.00000001')).toBe('0.00000000000001')
  })
  it('displays retained activity discounts from 1e-8 through 1e-20 exactly', () => {
    for (let exponent = 8; exponent <= 20; exponent++) {
      const expected = `0.${'0'.repeat(exponent - 1)}1`
      expect(factorText(`1e-${exponent}`)).toBe(expected)
      expect(ppmFactorText(`1e-${exponent - 6}`)).toBe(expected)
    }
  })
  it('keeps ordinary integers, zero and trailing zeros readable', () => {
    expect(ppmFactorText(1000000)).toBe('1')
    expect(ppmFactorText(1234567n)).toBe('1.234567')
    expect(ppmFactorText('9007199254740993')).toBe('9007199254.740993')
    expect(ppmFactorText('1000000.0000')).toBe('1')
    expect(ppmFactorText('12e6')).toBe('12')
    expect(ppmFactorText('0e-63')).toBe('0')
    expect(factorText('001.2500')).toBe('1.25')
  })
  it('rejects invalid, unsafe or excessively expanded values', () => {
    for (const value of [undefined, null, true, '', '-1', 'NaN', Infinity, 9007199254740992])
      expect(ppmFactorText(value)).toBe('—')
    expect(ppmFactorText('1e999999999')).toBe('—')
    expect(ppmFactorText('1'.repeat(4097))).toBe('—')
  })
})
