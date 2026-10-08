// Dependency-free SVG bar chart for request/spend trends. Accessible by
// design: the SVG is decorative (aria-hidden) and a visually-hidden table
// carries the same data for screen readers.
import { useTranslation } from '../../lib/i18n'

export type ChartPoint = { label: string; value: number }

export function BarChart(props: {
  title: string
  points: readonly ChartPoint[]
  valueLabel: string
  formatValue?: (value: number) => string
}) {
  const { t } = useTranslation()
  const format = props.formatValue ?? ((value: number) => value.toLocaleString())
  const max = Math.max(1, ...props.points.map((point) => point.value))
  const width = 640
  const height = 180
  const barGap = 6
  const barWidth = props.points.length > 0 ? width / props.points.length - barGap : 0
  const summary = t('{title}：{count} 个数据点，最大值 {max}', {
    title: t(props.title),
    count: props.points.length,
    max: format(max),
  })
  return (
    <figure className="analytics-chart">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={summary}
        className="analytics-chart-svg"
      >
        {props.points.map((point, index) => {
          const barHeight = max > 0 ? (point.value / max) * (height - 24) : 0
          const x = index * (barWidth + barGap)
          const y = height - barHeight
          return (
            <g key={point.label}>
              <rect
                x={x}
                y={y}
                width={Math.max(1, barWidth)}
                height={barHeight}
                rx={3}
                className="analytics-chart-bar"
              />
            </g>
          )
        })}
      </svg>
      <table className="sr-only">
        <caption>{t(props.title)}</caption>
        <thead>
          <tr>
            <th scope="col">{t('时间')}</th>
            <th scope="col">{t(props.valueLabel)}</th>
          </tr>
        </thead>
        <tbody>
          {props.points.map((point) => (
            <tr key={point.label}>
              <td>{point.label}</td>
              <td>{format(point.value)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  )
}
