import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'
import { useTranslation } from '../../lib/i18n'
import './chart.css'

export type ChartPoint = { label: string; value: number }

export function BarChart(props: {
  title: string
  points: readonly ChartPoint[]
  valueLabel: string
  formatValue?: (value: number) => string
}) {
  const { t } = useTranslation()
  const titleId = useId()
  const container = useRef<HTMLDivElement>(null)
  const [containerWidth, setContainerWidth] = useState(280)
  const [active, setActive] = useState<number | null>(null)
  useEffect(() => {
    const element = container.current
    if (!element) return
    const measure = () =>
      setContainerWidth(Math.max(280, Math.floor(element.getBoundingClientRect().width)))
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const format = props.formatValue ?? ((value: number) => value.toLocaleString())
  const values = props.points.map((point) =>
    Number.isFinite(point.value) ? Math.max(0, point.value) : 0,
  )
  const peak = Math.max(0, ...values)
  const max = peak > 0 ? peak : 1
  const integerAxis = !props.formatValue && values.every(Number.isInteger)
  const ticks = [
    ...new Set(
      [0, 1, 2, 3].map((index) => {
        const value = (max * index) / 3
        return integerAxis ? Math.round(value) : Number(value.toPrecision(3))
      }),
    ),
  ]
  const width =
    props.points.length > 14
      ? Math.max(containerWidth, props.points.length * 28 + 100)
      : containerWidth
  const height = 240
  const left = 76
  const right = 20
  const top = 22
  const bottom = 42
  const plotWidth = width - left - right
  const plotHeight = height - top - bottom
  const step = plotWidth / Math.max(1, props.points.length)
  const barWidth = Math.max(1, Math.min(48, step - Math.min(8, step / 3)))
  const maxLabels = Math.min(8, Math.max(2, Math.floor(plotWidth / 64)))
  const labelStep = Math.max(1, Math.ceil(props.points.length / maxLabels))
  const selected = props.points[active ?? props.points.length - 1]
  const dateLabel = (label: string) => (/^\d{4}-\d{2}-\d{2}$/.test(label) ? label.slice(5) : label)
  const move = (event: KeyboardEvent<SVGGElement>, index: number) => {
    let next: number
    if (event.key === 'ArrowRight') next = Math.min(props.points.length - 1, index + 1)
    else if (event.key === 'ArrowLeft') next = Math.max(0, index - 1)
    else if (event.key === 'Home') next = 0
    else if (event.key === 'End') next = props.points.length - 1
    else return
    event.preventDefault()
    event.currentTarget.parentElement
      ?.querySelector<SVGGElement>(`[data-chart-index="${next}"]`)
      ?.focus()
  }

  return (
    <figure className="analytics-chart analytics-chart-labelled">
      <figcaption className="analytics-chart-caption">
        <strong id={titleId}>{t(props.title)}</strong>
        <span className="analytics-chart-reading" aria-live="polite" aria-atomic="true">
          {selected ? (
            <>
              <bdi>{selected.label}</bdi>
              <span>{t(props.valueLabel)}</span>
              <strong className="tabular">{format(selected.value)}</strong>
            </>
          ) : (
            t('暂无数据')
          )}
        </span>
      </figcaption>
      <div className="analytics-chart-scroll" ref={container}>
        <svg
          viewBox={`0 0 ${width} ${height}`}
          role="group"
          aria-labelledby={titleId}
          className="analytics-chart-svg"
          style={{ width, height }}
        >
          {ticks.map((value) => {
            const y = top + plotHeight * (1 - value / max)
            return (
              <g key={value} aria-hidden="true">
                <line x1={left} x2={width - right} y1={y} y2={y} className="analytics-chart-grid" />
                <text x={left - 10} y={y + 4} textAnchor="end" className="analytics-chart-axis">
                  {format(value)}
                </text>
              </g>
            )
          })}
          {props.points.map((point, index) => {
            const barHeight = (values[index] / max) * plotHeight
            const x = left + index * step + (step - barWidth) / 2
            const y = top + plotHeight - barHeight
            const lastIndex = props.points.length - 1
            const showDate =
              index === lastIndex || (index % labelStep === 0 && lastIndex - index >= labelStep)
            return (
              <g
                key={`${point.label}-${index}`}
                role="img"
                tabIndex={0}
                aria-label={`${point.label} · ${t(props.valueLabel)} ${format(point.value)}`}
                data-chart-index={index}
                className="analytics-chart-point"
                onPointerEnter={() => setActive(index)}
                onPointerLeave={(event) => {
                  if (!event.currentTarget.matches(':focus')) setActive(null)
                }}
                onFocus={() => setActive(index)}
                onBlur={() => setActive(null)}
                onClick={() => setActive(index)}
                onKeyDown={(event) => move(event, index)}
              >
                <title>{`${point.label} · ${t(props.valueLabel)} ${format(point.value)}`}</title>
                <rect
                  x={x}
                  y={y}
                  width={barWidth}
                  height={barHeight}
                  rx={3}
                  className="analytics-chart-bar"
                />
                <rect
                  x={left + index * step}
                  y={top - 10}
                  width={step}
                  height={plotHeight + 20}
                  rx={4}
                  className="analytics-chart-hit"
                />
                {props.points.length <= 14 && step >= 32 && (
                  <text
                    x={x + barWidth / 2}
                    y={y - 7}
                    textAnchor="middle"
                    className="analytics-chart-value"
                    aria-hidden="true"
                  >
                    {format(point.value)}
                  </text>
                )}
                {showDate && (
                  <text
                    x={x + barWidth / 2}
                    y={height - 17}
                    textAnchor="middle"
                    className="analytics-chart-axis"
                    aria-hidden="true"
                  >
                    {dateLabel(point.label)}
                  </text>
                )}
              </g>
            )
          })}
        </svg>
      </div>
      <table className="sr-only">
        <caption>{t(props.title)}</caption>
        <thead>
          <tr>
            <th scope="col">{t('时间')}</th>
            <th scope="col">{t(props.valueLabel)}</th>
          </tr>
        </thead>
        <tbody>
          {props.points.map((point, index) => (
            <tr key={`${point.label}-${index}`}>
              <td>{point.label}</td>
              <td>{format(point.value)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  )
}
