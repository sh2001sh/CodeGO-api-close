import { describe, expect, it } from 'vitest'
import {
  invoicePayment,
  nonnegativeMicroCredits,
  positiveID,
  steppedMicroCredits,
  transferAmounts,
} from './amounts'

describe('transfer amount boundaries', () => {
  it('keeps money above JS safe integer precise and rounds fee upward', () => {
    expect(transferAmounts('9007199254.740993', 100)).toEqual({
      amount: 9007199254740993n,
      fee: 90071992547410n,
      total: 9097271247288403n,
    })
    expect(transferAmounts('0.010001', 100).fee).toBe(101n)
  })
  it('rejects precision loss, zero and total integer overflow', () => {
    for (const value of ['0', '1.0000001', '-1', '9e12', '9223372036854.775807'])
      expect(() => transferAmounts(value, 100)).toThrow()
    expect(() => transferAmounts('1', -1)).toThrow()
  })
  it('formats invoices by currency without floating point money', () => {
    expect(invoicePayment('9007199254740993', 'usd')).toBe('USD 90,071,992,547,409.93')
    expect(invoicePayment(1234, 'jpy')).toBe('JPY 1,234')
    expect(invoicePayment(1234, 'kwd')).toBe('KWD 1.234')
  })
  it('accepts zero admin allowance and exact integer identities without rounding', () => {
    expect(nonnegativeMicroCredits('0.000000')).toBe(0n)
    expect(nonnegativeMicroCredits('9007199254.740993')).toBe(9007199254740993n)
    expect(positiveID('09223372036854775807')).toBe('9223372036854775807')
    for (const value of ['0', '-1', '1.1', '9223372036854775808'])
      expect(() => positiveID(value)).toThrow()
    expect(() => nonnegativeMicroCredits('-0.01')).toThrow()
  })
  it('checks fuel minimum and step without rounding large credits', () => {
    expect(steppedMicroCredits('9007199254.740993', 10000, 1)).toBe(9007199254740993n)
    expect(steppedMicroCredits('0.02', 10000, 10000)).toBe(20000n)
    expect(() => steppedMicroCredits('0.009999', 10000, 1)).toThrow('最低')
    expect(() => steppedMicroCredits('0.010001', 10000, 10000)).toThrow('步长')
    expect(() => steppedMicroCredits('1', 1, 0)).toThrow('配置')
  })
})
