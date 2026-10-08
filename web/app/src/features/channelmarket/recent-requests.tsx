import { useTranslation } from '../../lib/i18n'
import type { Schema } from '../../lib/types'

export type RequestTone = 'good' | 'warning' | 'poor' | 'empty'
export function requestTone(count: number | string | bigint, rate: number): RequestTone {
  if (BigInt(count) === 0n) return 'empty'
  return rate > 90 ? 'good' : rate >= 75 ? 'warning' : 'poor'
}
const labels: Record<RequestTone, string> = {
  good: '成功率高于 90%',
  warning: '成功率 75%–90%',
  poor: '成功率低于 75%',
  empty: '无请求',
}

/** Only persisted request buckets supplied by the server; probe results are unrelated. */
export function RecentRequests(props: { group: Schema['ChannelMarketChannelView'] }) {
  const { t, locale } = useTranslation()
  const series = props.group.recent_request_series
  if (!series?.length)
    return <p className="market-recent-empty subtle">{t('暂无近 6 小时请求统计')}</p>
  const bucket = props.group.recent_request_bucket_seconds || 3600
  const time = new Intl.DateTimeFormat(locale, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
  const percent = new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 2 })
  return (
    <div className="market-recent">
      <div className="market-recent-heading">
        <span>{t('近 6 小时请求')}</span>
        <span className="subtle">{t('每格 1 小时')}</span>
      </div>
      <ol className="market-recent-slots" aria-label={t('近 6 小时请求')}>
        {series.slice(-6).map((item) => {
          const tone = requestTone(item.request_count, item.success_rate)
          const start = Number(item.ts) * 1000
          const detail = `${time.format(start)} – ${time.format(start + bucket * 1000)} · ${t('请求数')} ${String(item.request_count)} · ${BigInt(item.request_count) === 0n ? t('无请求') : `${t('成功率')} ${percent.format(item.success_rate / 100)}`}`
          return (
            <li key={String(item.ts)}>
              <span
                className="market-recent-slot"
                data-tone={tone}
                role="img"
                tabIndex={0}
                title={detail}
                aria-label={detail}
              />
            </li>
          )
        })}
      </ol>
      <ul className="market-recent-legend">
        {(['good', 'warning', 'poor', 'empty'] as const).map((tone) => (
          <li key={tone}>
            <span data-tone={tone} aria-hidden />
            {t(labels[tone])}
          </li>
        ))}
      </ul>
    </div>
  )
}
