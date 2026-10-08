import { describe, expect, it } from 'vitest'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { IncomeTrend } from './owner-analytics'
import {
  analyticsQuery,
  analyticsRange,
  localDateTime,
  ownerLogCursor,
  successPercent,
  trendHeight,
} from './owner-analytics-format'

describe('owner analytics financial and filter boundaries', () => {
  it('renders exact money in accessible trend data without converting it to floating point', () => {
    const html = renderToStaticMarkup(
      createElement(IncomeTrend, {
        points: [
          {
            timestamp: '2026-10-07T00:00:00Z',
            request_count: 1n,
            success_count: 1n,
            gross_micro: 9_007_199_254_740_993n,
            commission_micro: 0n,
            fee_micro: 0n,
            net_micro: 9_007_199_254_740_993n,
          },
        ],
      }),
    )
    expect(html).toContain('9,007,199,254.740993 credits')
    expect(html).toContain('class="sr-only"')
  })

  it('shows an explicit empty income state for absent samples', () => {
    const html = renderToStaticMarkup(createElement(IncomeTrend, { points: [] }))
    expect(html).toContain('所选时段暂无收入')
    expect(html).not.toContain('<rect')
  })

  it('preserves int64 precision when plotting values larger than the JS safe range', () => {
    const maximum = 9_223_372_036_854_775_806n
    expect(trendHeight(maximum / 2n, maximum)).toBe(0.5)
    expect(trendHeight(maximum, maximum)).toBe(1)
    expect(trendHeight(0n, 0n)).toBe(0)
  })

  it('does not present an empty success sample as zero or perfect success', () => {
    expect(successPercent(0n, 0n)).toBe('—')
    expect(successPercent('90071992547409930', '90071992547409930')).toBe('100%')
    expect(successPercent(1n, 3n)).toBe('33.33%')
  })

  it('rejects equal, reversed and invalid date ranges before changing applied filters', () => {
    expect(() => analyticsRange('2026-10-07T10:00', '2026-10-07T10:00')).toThrow()
    expect(() => analyticsRange('2026-10-08T10:00', '2026-10-07T10:00')).toThrow()
    expect(() => analyticsRange('', '2026-10-07T10:00')).toThrow()
    expect(() => analyticsRange('2025-01-01T00:00Z', '2026-01-03T00:00Z')).toThrow('366')
  })

  it('converts local date inputs to explicit API instants', () => {
    const start = new Date(2026, 9, 1, 12, 30)
    const end = new Date(2026, 9, 7, 12, 30)
    expect(analyticsRange(localDateTime(start), localDateTime(end))).toEqual({
      from: start.toISOString(),
      to: end.toISOString(),
    })
  })

  it('uses identical, encoded filters in export URLs including model punctuation', () => {
    const filters = {
      from: '2026-10-01T00:00:00Z',
      to: '2026-10-07T00:00:00Z',
      channel_id: '123',
      model: 'vendor/model:a+b',
    }
    const query = new URLSearchParams(analyticsQuery(filters).slice(1))
    expect(Object.fromEntries(query)).toEqual(filters)
    expect(analyticsQuery()).toBe('')
  })

  it('retains timestamp and exact ID to avoid dropping same-time logs at page boundaries', () => {
    const timestamp = '2026-10-07T00:00:00.123456Z'
    const first = ownerLogCursor({ created_at: timestamp, id: 9_007_199_254_740_993n })
    const second = ownerLogCursor({ created_at: timestamp, id: 9_007_199_254_740_992n })
    expect(first.before).toBe(second.before)
    expect(first.before_id).toBe(9_007_199_254_740_993n)
    expect(first).not.toEqual(second)
  })
})
