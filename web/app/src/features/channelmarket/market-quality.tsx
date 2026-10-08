import { useTranslation } from '../../lib/i18n'
import type { Schema } from '../../lib/types'
import { groupProbe, toneLabel } from '../public/status-helpers'
import { Status } from '../../components/ui'

export function currentQuality(group: Schema['ChannelMarketChannelView'], now = Date.now()) {
  if (!group.quality) return undefined
  const captured = Date.parse(group.quality.calculated_at)
  return Number.isFinite(captured) && now - captured <= 60 * 60_000 && captured <= now + 5 * 60_000
    ? group.quality
    : undefined
}

export function MarketQuality(props: {
  group: Schema['ChannelMarketChannelView']
  expanded?: boolean
}) {
  const { t, locale } = useTranslation()
  const group = props.group
  const probe = groupProbe(group)
  const quality = currentQuality(group)
  const ratio = (value: number | undefined) =>
    value === undefined
      ? t('暂无数据')
      : new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 2 }).format(value)
  return (
    <>
      <dl className="market-service-facts">
        <div>
          <dt>{t('最近探测状态')}</dt>
          <dd>
            <Status value={toneLabel[probe.tone]} />
          </dd>
        </div>
        <div>
          <dt>{t('探测延迟')}</dt>
          <dd>{probe.latencyMs === null ? t('暂无数据') : `${probe.latencyMs} ms`}</dd>
        </div>
        <div>
          <dt>{t('24 小时请求')}</dt>
          <dd>{quality ? String(quality.request_count) : t('暂无数据')}</dd>
        </div>
        <div>
          <dt>{t('调用成功率')}</dt>
          <dd>{ratio(quality?.success_rate)}</dd>
        </div>
        {props.expanded && (
          <>
            <div>
              <dt>{t('最近探测')}</dt>
              <dd>
                {probe.testedAt ? new Date(probe.testedAt).toLocaleString(locale) : t('暂无数据')}
              </dd>
            </div>
            <div>
              <dt>{t('并发上限')}</dt>
              <dd>{group.max_concurrency > 0 ? group.max_concurrency : t('未设上限')}</dd>
            </div>
            <div>
              <dt>{t('单用户并发')}</dt>
              <dd>{group.user_max_concurrency > 0 ? group.user_max_concurrency : t('未设上限')}</dd>
            </div>
            {group.maintenance_window && (
              <div>
                <dt>{t('维护窗口')}</dt>
                <dd>{group.maintenance_window}</dd>
              </div>
            )}
          </>
        )}
      </dl>
      {quality ? (
        props.expanded && (
          <div className="market-quality-snapshot">
            <dl className="market-service-facts">
              <div>
                <dt>{t('缓存命中率')}</dt>
                <dd>{ratio(quality.cache_hit_rate)}</dd>
              </div>
              <div>
                <dt>{t('平均实扣 credits')}</dt>
                <dd>
                  <bdi dir="ltr">{quality.average_charge_credits ?? t('暂无数据')}</bdi>
                </dd>
              </div>
              <div>
                <dt>{t('综合评分')}</dt>
                <dd>{quality.observing ? t('样本不足') : quality.score.toFixed(2)}</dd>
              </div>
            </dl>
            <p className="subtle">
              {t('调用统计更新于')} {new Date(quality.calculated_at).toLocaleString(locale)} ·{' '}
              {t('探测延迟不等同于实际调用首字时间。')}
            </p>
          </div>
        )
      ) : (
        <p className="subtle market-quality-empty">
          {t(
            group.quality
              ? '调用统计已过期，等待更新。'
              : '暂无调用统计；探测结果仅反映最近一次连接检查。',
          )}
        </p>
      )}
    </>
  )
}
