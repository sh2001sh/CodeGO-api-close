import { useState, type ReactNode } from 'react'
import { useQueries } from '@tanstack/react-query'
import { Button, ErrorMessage, Loading } from '../../components/ui'
import { useTranslation } from '../../lib/i18n'
import type { Schema } from '../../lib/types'
import { ModelGroupPrice } from '../public/model-group-price'
import { defaultEstimateUsage, estimateMarketCost, type EstimateUsage } from './cost-estimate'
import { effectiveGroupQuote } from './market-pricing'
import { ChannelDisclosure, modelInsightsOptions } from './model-insights'
import '../../styles/market-comparison.css'

type Group = Schema['ChannelMarketChannelView']

export function MarketComparison(props: {
  groups: Group[]
  model: string
  onRemove: (group: string) => void
  onClear: () => void
  onInspect: (group: Group) => void
}) {
  const { t, locale } = useTranslation()
  const [windowHours, setWindowHours] = useState<24 | 168>(24)
  const [usage, setUsage] = useState<EstimateUsage>(defaultEstimateUsage)
  const queries = useQueries({
    queries: props.groups.map((group) =>
      modelInsightsOptions(group.group_id, props.model, windowHours),
    ),
  })
  const ratio = (value: number | null | undefined) =>
    value == null
      ? t('暂无数据')
      : new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 2 }).format(value)
  const metric = (value: number | null | undefined, unit: string) =>
    value == null
      ? t('暂无数据')
      : `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value)} ${unit}`
  const count = (value: number | string | bigint | undefined) =>
    value == null ? t('暂无数据') : String(value)
  const fields: [keyof EstimateUsage, string][] = [
    ['requests', '预计请求数'],
    ['input', '每次非缓存输入 tokens'],
    ['output', '每次输出 tokens'],
    ['cacheRead', '每次缓存读取 tokens'],
    ['cacheWrite', '每次缓存写入 tokens'],
    ['units', '每次计费单位数'],
  ]
  const quotes = props.groups.map((group) => effectiveGroupQuote(group, props.model))
  const row = (label: string, values: ReactNode[]) => (
    <tr>
      <th scope="row">{t(label)}</th>
      {values.map((value, index) => (
        <td key={props.groups[index].group_id}>{value}</td>
      ))}
    </tr>
  )
  const stats = (render: (data: Schema['ChannelMarketInsights'] | undefined) => ReactNode) =>
    queries.map((query) =>
      query.error ? t('加载失败') : query.isPending ? t('加载中') : render(query.data),
    )
  return (
    <section className="market-comparison" aria-labelledby="market-comparison-heading">
      <header className="market-comparison-heading">
        <div>
          <h2 id="market-comparison-heading">{t('同模型分组比较')}</h2>
          <p>
            <bdi dir="ltr">{props.model}</bdi> · {props.groups.length} {t('个分组')}
          </p>
        </div>
        <Button variant="secondary" onClick={props.onClear}>
          {t('清空比较')}
        </Button>
      </header>
      <p className="subtle">
        {t(
          props.groups.length < 2
            ? '再选择一个支持相同模型的分组，比较报价、真实调用与数据声明。'
            : '最多比较四个分组。费用使用当前报价，服务质量按相同模型与统计窗口展示。',
        )}
      </p>
      <div className="market-estimate-fields">
        {fields.map(([key, label]) => (
          <label className="field" key={key} htmlFor={`estimate-${key}`}>
            <span>{t(label)}</span>
            <input
              id={`estimate-${key}`}
              inputMode="numeric"
              pattern="[0-9]*"
              value={usage[key]}
              onChange={(event) =>
                setUsage((previous) => ({ ...previous, [key]: event.target.value }))
              }
              aria-describedby="market-estimate-note"
            />
          </label>
        ))}
        <label className="field" htmlFor="compare-window">
          <span>{t('统计窗口')}</span>
          <select
            id="compare-window"
            value={windowHours}
            onChange={(event) => setWindowHours(event.target.value === '168' ? 168 : 24)}
          >
            <option value="24">{t('最近 24 小时')}</option>
            <option value="168">{t('最近 7 天')}</option>
          </select>
        </label>
      </div>
      <p className="subtle" id="market-estimate-note">
        {t(
          '输入与缓存 tokens 分开填写；按次、图片、音频或视频价格使用对应计费单位数。估算已包含分组倍率，实际账单受真实用量、缓存与计价规则影响。',
        )}
      </p>
      {queries.map(
        (query, index) =>
          query.error && (
            <div key={props.groups[index].group_id}>
              <strong>{props.groups[index].system_display_name}</strong>
              <ErrorMessage error={query.error} />
              <Button variant="secondary" onClick={() => void query.refetch()}>
                {t('重试')}
              </Button>
            </div>
          ),
      )}
      <div
        className="market-comparison-scroll"
        role="region"
        aria-label={t('分组比较表')}
        tabIndex={0}
      >
        <table className="market-comparison-table">
          <caption>
            {t('同模型分组比较')} · {props.model}
          </caption>
          <thead>
            <tr>
              <th scope="col">{t('比较项目')}</th>
              {props.groups.map((group) => (
                <th scope="col" key={group.group_id}>
                  <strong>{group.system_display_name}</strong>
                  <span className="subtle">
                    {t('分组 ID')} <bdi dir="ltr">{group.id}</bdi>
                  </span>
                  <Button
                    variant="quiet"
                    size="sm"
                    onClick={() => props.onRemove(group.group_id)}
                    aria-label={`${t('移除比较')} ${group.system_display_name}`}
                  >
                    {t('移除比较')}
                  </Button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {row(
              '当前模型报价',
              quotes.map((quote) =>
                quote ? <ModelGroupPrice group={quote} /> : t('暂未提供价格'),
              ),
            )}
            {row(
              '预估总费用',
              quotes.map((quote) => {
                const result = estimateMarketCost(quote?.price, usage)
                return result.kind === 'known' ? (
                  <strong className="market-estimate-amount">
                    <bdi dir="ltr">{result.credits} credits</bdi>
                  </strong>
                ) : (
                  t(
                    result.kind === 'invalid'
                      ? '请输入有效非负整数，最多一万亿。'
                      : result.kind === 'dynamic'
                        ? '动态计价无法预估'
                        : '报价不足，无法预估',
                  )
                )
              }),
            )}
            {row(
              '计费单位',
              quotes.map((quote) =>
                !quote?.price ? (
                  t('暂无数据')
                ) : quote.price.mode === 'per_request' ? (
                  <bdi dir="ltr">{quote.price.unit}</bdi>
                ) : quote.price.mode === 'token' || quote.price.mode === 'per_token' ? (
                  'tokens'
                ) : (
                  t('按动态规则计价')
                ),
              ),
            )}
            {row(
              '模型请求样本',
              stats((data) => count(data?.request_count)),
            )}
            {row(
              '独立调用用户',
              stats((data) => count(data?.independent_consumers)),
            )}
            {row(
              '模型成功率',
              stats((data) => ratio(data?.success_rate)),
            )}
            {row(
              '成功率置信下界',
              stats((data) => ratio(data?.wilson_success_rate)),
            )}
            {row(
              '首字时间 P50',
              stats((data) => metric(data?.ttft_p50_ms, 'ms')),
            )}
            {row(
              '首字时间 P95',
              stats((data) => metric(data?.ttft_p95_ms, 'ms')),
            )}
            {row(
              '平均输出速度',
              stats((data) => metric(data?.avg_tps, 'tokens/s')),
            )}
            {row(
              '性能样本',
              stats((data) => count(data?.performance_samples)),
            )}
            {row(
              '渠道限制',
              props.groups.map((group) => (
                <dl className="model-price-lines">
                  <div>
                    <dt>{t('并发上限')}</dt>
                    <dd>{group.max_concurrency > 0 ? group.max_concurrency : t('未设上限')}</dd>
                  </div>
                  <div>
                    <dt>{t('单用户并发')}</dt>
                    <dd>
                      {group.user_max_concurrency > 0 ? group.user_max_concurrency : t('未设上限')}
                    </dd>
                  </div>
                  <div>
                    <dt>{t('每秒请求上限')}</dt>
                    <dd>{group.qps > 0 ? group.qps : t('未设上限')}</dd>
                  </div>
                  {group.maintenance_window && (
                    <div>
                      <dt>{t('维护窗口')}</dt>
                      <dd>{group.maintenance_window}</dd>
                    </div>
                  )}
                </dl>
              )),
            )}
            {row(
              '来源与数据声明',
              queries.map((query) =>
                query.isPending ? (
                  <Loading />
                ) : query.error ? (
                  t('加载失败')
                ) : (
                  <ChannelDisclosure disclosure={query.data?.disclosure} model={props.model} />
                ),
              ),
            )}
            {row(
              '操作',
              props.groups.map((group) => (
                <Button variant="secondary" onClick={() => props.onInspect(group)}>
                  {t('选择此分组')}
                </Button>
              )),
            )}
          </tbody>
        </table>
      </div>
      <p className="subtle">
        {t(
          '无样本不代表零延迟或完美成功率。首字时间与输出速度仅展示有真实计时数据的请求；声明由渠道主提供。',
        )}
      </p>
    </section>
  )
}
