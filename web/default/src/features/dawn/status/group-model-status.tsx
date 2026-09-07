import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { getMarketplaceGroupModelStatus } from '@/features/marketplace/api'
import { RecentRequestStrip } from '@/features/marketplace/components/recent-request-strip'
import type { MarketplaceGroup } from '@/features/marketplace/types'

export function GroupModelStatus(props: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const userID = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: [
      'marketplace-group-model-status',
      props.group.public_slug,
      userID,
    ],
    queryFn: () => getMarketplaceGroupModelStatus(props.group.public_slug),
    staleTime: 30_000,
    refetchInterval: 60_000,
    refetchIntervalInBackground: false,
  })

  if (query.isPending) {
    return (
      <p className='px-6 py-5 text-sm' role='status'>
        {t('正在加载各模型请求状态…')}
      </p>
    )
  }
  if (query.isError) {
    return (
      <div
        className='flex flex-wrap items-center gap-3 px-6 py-5 text-sm'
        role='alert'
      >
        <span>{t('模型请求状态加载失败')}</span>
        <button
          type='button'
          className='btn mini'
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
        >
          {t('重试')}
        </button>
      </div>
    )
  }
  if (!query.data.length) {
    return <p className='px-6 py-5 text-sm'>{t('该分组暂无可用模型')}</p>
  }

  return (
    <div>
      <p className='px-4 pt-4 text-xs text-[var(--dawn-ink2)] sm:px-6'>
        {t('以下状态按模型的真实请求分别统计；最近检测仅供参考。')}
      </p>
      {query.data.map((model) => {
        const verification = props.group.model_verification_results.find(
          (item) => item.model === model.model
        )
        return (
          <article
            key={model.model}
            className='model-status-row min-w-0 border-b border-[var(--dawn-line)] px-4 py-4 last:border-0 sm:px-6'
            aria-label={model.model}
          >
            <div className='mb-2 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-2'>
              <h4 className='min-w-0 font-mono text-sm font-semibold break-all'>
                {model.model}
              </h4>
              <span className='text-xs text-[var(--dawn-ink2)]'>
                {model.request_count > 0
                  ? t('近 6 小时 {{count}} 次请求 · 成功率 {{rate}}%', {
                      count: model.request_count,
                      rate: model.success_rate.toFixed(1),
                    })
                  : t('近 6 小时暂无请求')}
              </span>
            </div>
            <RecentRequestStrip group={model} />
            <div className='mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-[var(--dawn-ink2)]'>
              {verification ? (
                <>
                  <span>
                    {t('最近检测')} ·{' '}
                    {verification.status === 'passed' ? t('通过') : t('失败')}
                  </span>
                  <span>
                    {t('检测延迟')} {verification.latency_ms} ms
                  </span>
                  <time dateTime={verification.tested_at}>
                    {verification.tested_at
                      ? new Date(verification.tested_at).toLocaleString()
                      : '—'}
                  </time>
                </>
              ) : (
                <span>{t('暂无模型检测记录')}</span>
              )}
            </div>
          </article>
        )
      })}
    </div>
  )
}
