import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useTranslation } from '../../lib/i18n'
import type { Schema } from '../../lib/types'
import { Button, ErrorMessage, Loading } from '../../components/ui'

export function modelInsightsOptions(group: string, model: string, windowHours: 24 | 168) {
  return {
    ...resourceOptions(
      'market-model-insights',
      (signal) =>
        api
          .GET('/api/marketplace/groups/{id}/insights', {
            signal,
            params: { path: { id: group }, query: { model, window_hours: windowHours } },
          })
          .then(unwrap),
      [group, model, windowHours],
    ),
    staleTime: 60_000,
  }
}

export const sourceLabels = {
  unknown: '未披露',
  direct: '直接接入',
  reseller: '转售接入',
  self_hosted: '自行部署',
} as const
export const capabilityLabels = {
  unknown: '未披露',
  supported: '支持',
  unsupported: '不支持',
} as const

const failureLabels: Record<string, string> = {
  rate_limited: '上游限流',
  timeout: '请求超时',
  upstream_auth: '上游认证失败',
  upstream_error: '上游服务错误',
  rejected: '请求被拒绝',
  other: '其他失败',
}

export function ChannelDisclosure(props: {
  disclosure?: Schema['ChannelMarketDisclosure'] | null
  model: string
}) {
  const { t, locale } = useTranslation()
  const disclosure = props.disclosure
  const capabilities = disclosure?.models.find((item) => item.model === props.model)
  return (
    <section className="market-disclosure" aria-label={t('来源与数据声明')}>
      <h4>{t('来源与数据声明')}</h4>
      {!disclosure ? (
        <p className="subtle">{t('渠道主尚未披露来源、数据处理与模型能力。')}</p>
      ) : (
        <>
          <dl className="market-disclosure-facts">
            <div>
              <dt>{t('来源')}</dt>
              <dd>{t(sourceLabels[disclosure.source_kind])}</dd>
            </div>
            <div>
              <dt>{t('处理地区')}</dt>
              <dd>{disclosure.regions.length ? disclosure.regions.join(' / ') : t('未披露')}</dd>
            </div>
            <div>
              <dt>{t('数据留存')}</dt>
              <dd>
                {t(
                  disclosure.retention === 'none'
                    ? '声明不留存'
                    : disclosure.retention === 'limited'
                      ? '有限留存'
                      : '未披露',
                )}
                {disclosure.retention === 'limited' && disclosure.retention_days != null
                  ? ` · ${disclosure.retention_days} ${t('天')}`
                  : ''}
              </dd>
            </div>
            <div>
              <dt>{t('用于训练')}</dt>
              <dd>
                {t(
                  disclosure.training === 'no'
                    ? '声明不用于训练'
                    : disclosure.training === 'yes'
                      ? '可能用于训练'
                      : '未披露',
                )}
              </dd>
            </div>
            {(
              [
                ['流式输出', capabilities?.streaming],
                ['工具调用', capabilities?.tools],
                ['结构化输出', capabilities?.structured_outputs],
                ['视觉输入', capabilities?.vision],
              ] as const
            ).map(([label, value]) => (
              <div key={label}>
                <dt>{t(label)}</dt>
                <dd>{t(capabilityLabels[value ?? 'unknown'])}</dd>
              </div>
            ))}
            <div>
              <dt>{t('上下文长度')}</dt>
              <dd>
                {capabilities?.context_tokens == null
                  ? t('未披露')
                  : String(capabilities.context_tokens)}
              </dd>
            </div>
            <div>
              <dt>{t('最大输出 tokens')}</dt>
              <dd>
                {capabilities?.max_output_tokens == null
                  ? t('未披露')
                  : String(capabilities.max_output_tokens)}
              </dd>
            </div>
          </dl>
          {disclosure.policy_url && (
            <a href={disclosure.policy_url} target="_blank" rel="noopener noreferrer">
              {t('查看渠道数据政策')}
            </a>
          )}
          <p className="subtle">
            {t('声明更新于')} {new Date(disclosure.updated_at).toLocaleString(locale)}
          </p>
        </>
      )}
      <p className="subtle">
        {t(
          '以上为渠道主自行声明；连通验证仅说明请求可达，来源授权、模型真实性与数据策略未经平台独立核验。',
        )}
      </p>
    </section>
  )
}

export function ModelInsights(props: { group: string; model: string }) {
  const { t, locale } = useTranslation()
  const query = useQuery(modelInsightsOptions(props.group, props.model, 24))
  const insight = query.data
  const ratio = (value: number | null | undefined) =>
    value == null
      ? t('暂无数据')
      : new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 2 }).format(value)
  const metric = (value: number | null | undefined, unit: string) =>
    value == null
      ? t('暂无数据')
      : `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value)} ${unit}`
  return (
    <section className="market-model-insights" aria-label={t('所选模型服务质量')}>
      <h3>
        {t('所选模型服务质量')} · <bdi dir="ltr">{props.model}</bdi>
      </h3>
      <ErrorMessage error={query.error} />
      {query.error && (
        <Button variant="secondary" onClick={() => void query.refetch()}>
          {t('重试')}
        </Button>
      )}
      {query.isPending && <Loading />}
      {insight && (
        <>
          <dl className="market-disclosure-facts">
            <div>
              <dt>{t('24 小时请求')}</dt>
              <dd>{String(insight.request_count)}</dd>
            </div>
            <div>
              <dt>{t('独立调用用户')}</dt>
              <dd>{String(insight.independent_consumers)}</dd>
            </div>
            <div>
              <dt>{t('模型成功率')}</dt>
              <dd>{ratio(insight.success_rate)}</dd>
            </div>
            <div>
              <dt>{t('成功率置信下界')}</dt>
              <dd>{ratio(insight.wilson_success_rate)}</dd>
            </div>
            <div>
              <dt>{t('首字时间 P50')}</dt>
              <dd>{metric(insight.ttft_p50_ms, 'ms')}</dd>
            </div>
            <div>
              <dt>{t('首字时间 P95')}</dt>
              <dd>{metric(insight.ttft_p95_ms, 'ms')}</dd>
            </div>
            <div>
              <dt>{t('平均输出速度')}</dt>
              <dd>{metric(insight.avg_tps, 'tokens/s')}</dd>
            </div>
            <div>
              <dt>{t('性能样本')}</dt>
              <dd>{String(insight.performance_samples)}</dd>
            </div>
          </dl>
          {BigInt(insight.request_count) === 0n && (
            <p className="subtle">{t('所选模型暂无有效调用样本，无法判断实际服务质量。')}</p>
          )}
          <p className="subtle">
            {t('指标来自所选模型的真实调用；首字时间仅统计有计时数据的流式请求，探测延迟不参与。')}
          </p>
          {insight.failure_counts.length > 0 && (
            <details className="market-insight-failures">
              <summary>{t('失败类型')}</summary>
              <ul>
                {insight.failure_counts.map((failure) => (
                  <li key={failure.category}>
                    {t(failureLabels[failure.category] ?? '其他失败')} · {String(failure.count)}
                  </li>
                ))}
              </ul>
            </details>
          )}
          <ChannelDisclosure disclosure={insight.disclosure} model={props.model} />
        </>
      )}
    </section>
  )
}
