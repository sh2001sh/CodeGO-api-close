import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { BarChart } from './chart'

describe('usage chart', () => {
  it('renders dates and values as visible text, with focusable data points', () => {
    const html = renderToStaticMarkup(
      createElement(BarChart, {
        title: '每日请求数',
        valueLabel: '请求数',
        points: [
          { label: '2026-10-14', value: 12 },
          { label: '2026-10-15', value: 27 },
        ],
      }),
    )
    const drawing = html.slice(html.indexOf('<svg'), html.indexOf('</svg>'))
    expect(drawing).toContain('>10-14</text>')
    expect(drawing).toContain('>10-15</text>')
    expect(drawing).toContain('>27</text>')
    expect(drawing).toContain('aria-label="2026-10-14 · 请求数 12"')
    expect(drawing.match(/tabindex="0"/g)).toHaveLength(2)
    expect(html).toContain('<figcaption')
    expect(html).toContain('aria-live="polite"')
  })

  it('keeps zero-value and empty series valid', () => {
    const zero = renderToStaticMarkup(
      createElement(BarChart, {
        title: '每日请求数',
        valueLabel: '请求数',
        points: [{ label: '2026-10-15', value: 0 }],
      }),
    )
    expect(zero).toContain('height="0"')
    expect(zero).not.toMatch(/(?:NaN|Infinity)/)
    const empty = renderToStaticMarkup(
      createElement(BarChart, {
        title: '每日请求数',
        valueLabel: '请求数',
        points: [],
      }),
    )
    expect(empty).toContain('暂无数据')
    expect(empty).not.toMatch(/(?:NaN|Infinity)/)
  })

  it('keeps long series inspectable without overlapping every date label', () => {
    const html = renderToStaticMarkup(
      createElement(BarChart, {
        title: '每日请求数',
        valueLabel: '请求数',
        points: Array.from({ length: 50 }, (_, i) => ({ label: `day-${i}`, value: i })),
      }),
    )
    const drawing = html.slice(html.indexOf('<svg'), html.indexOf('</svg>'))
    expect(drawing.match(/tabindex="0"/g)).toHaveLength(50)
    expect(drawing.match(/class="analytics-chart-axis"/g)?.length).toBeLessThan(15)
    expect(drawing).toContain('>day-49</text>')
  })

  it('keeps a single request bar narrow with unique integer ticks and a mobile-width canvas', () => {
    const html = renderToStaticMarkup(
      createElement(BarChart, {
        title: '每日请求数',
        valueLabel: '请求数',
        points: [{ label: '2026-10-15', value: 1 }],
      }),
    )
    const drawing = html.slice(html.indexOf('<svg'), html.indexOf('</svg>'))
    expect(drawing).toContain('viewBox="0 0 280 240"')
    expect(drawing).not.toContain('min-width')
    expect(drawing).toMatch(/width="48"[^>]*class="analytics-chart-bar"/)
    expect(drawing).not.toMatch(/>0\.(33|67)</)
    const axis = [...drawing.matchAll(/class="analytics-chart-axis"[^>]*>([^<]+)</g)].map(
      (match) => match[1],
    )
    expect(axis).toEqual(['0', '1', '10-15'])
  })

  it('preserves fractional amount ticks', () => {
    const html = renderToStaticMarkup(
      createElement(BarChart, {
        title: '扣费合计',
        valueLabel: 'credits',
        formatValue: (value) => value.toFixed(2),
        points: [{ label: '2026-10-15', value: 0.6 }],
      }),
    )
    const drawing = html.slice(html.indexOf('<svg'), html.indexOf('</svg>'))
    expect(drawing).toContain('>0.20</text>')
    expect(drawing).toContain('>0.40</text>')
    expect(drawing).toContain('>0.60</text>')
  })
})
