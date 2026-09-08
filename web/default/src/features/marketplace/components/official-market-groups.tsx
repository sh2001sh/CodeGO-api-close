import { useState } from 'react'
import { BadgeCheck, ChevronDown, CircleDashed } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getMarketplaceAutoRoutePool, getMarketplaceRoutePool } from '../api'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceAutoRoutePoolUpdate,
  useMarketplaceRoutePool,
  useMarketplaceRoutePoolUpdate,
} from '../hooks'
import { selectedAutoRoutePoolGroupIDs } from '../lib/auto-route-pool'
import type { MarketplaceAutoRoutePoolItem } from '../types'

// Official IDs come from the routing catalog, not marketplace channel IDs.
export function OfficialMarketGroups(props: {
  poolID: string
  enabled?: boolean
}) {
  const { t } = useTranslation()
  const catalog = useMarketplaceAutoRoutePool(props.enabled ?? true)
  const isAuto = props.poolID === 'auto'
  const pool = useMarketplaceRoutePool(isAuto ? '' : props.poolID)
  const updateAuto = useMarketplaceAutoRoutePoolUpdate()
  const updatePool = useMarketplaceRoutePoolUpdate()
  const [pending, setPending] = useState(false)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const target = isAuto ? catalog : pool
  const selected = new Set(
    selectedAutoRoutePoolGroupIDs(target.data?.items ?? [])
  )
  const items = (catalog.data?.items ?? []).filter(
    (item) => item.source_type === 'official'
  )

  const add = async (item: MarketplaceAutoRoutePoolItem) => {
    setPending(true)
    try {
      // Refresh before replacing membership so adding an official group preserves
      // existing marketplace members, their order, and the saved pool strategy.
      const latest = isAuto
        ? await getMarketplaceAutoRoutePool()
        : await getMarketplaceRoutePool(props.poolID)
      const ids = selectedAutoRoutePoolGroupIDs(latest.items)
      if (ids.includes(item.group_id)) return
      if (ids.length >= 10) throw new Error(t('路由池最多可添加 10 个分组'))
      if (
        latest.config.max_multiplier > 0 &&
        item.multiplier > latest.config.max_multiplier
      ) {
        throw new Error(t('该分组倍率超过当前路由池上限'))
      }
      const values = {
        groupIds: [...ids, item.group_id],
        config: latest.config,
      }
      if (isAuto) await updateAuto.mutateAsync(values)
      else await updatePool.mutateAsync({ id: props.poolID, ...values })
      toast.success(t('已将官方分组加入路由池'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('加入路由池失败'))
    } finally {
      setPending(false)
    }
  }

  if (props.enabled === false) return null
  return (
    <section className='official-market-groups' aria-label={t('官方分组')}>
      <div className='official-market-heading'>
        <div>
          <span>{t('官方分组')}</span>
          <p>{t('当前账号可用，可与市场渠道一起加入路由池。')}</p>
        </div>
        {!catalog.isLoading && !catalog.isError && <b>{items.length}</b>}
      </div>
      {catalog.isLoading && (
        <div className='empty official-market-empty' role='status'>
          <span className='eic'>
            <CircleDashed size={20} className='animate-pulse' />
          </span>
          <b>{t('正在加载官方分组…')}</b>
        </div>
      )}
      {catalog.isError && (
        <div className='empty official-market-empty'>
          <b>{t('官方分组加载失败')}</b>
          <button className='btn mini' onClick={() => void catalog.refetch()}>
            {t('重试')}
          </button>
        </div>
      )}
      {!catalog.isLoading && !catalog.isError && items.length === 0 && (
        <div className='empty official-market-empty'>
          <b>{t('当前账号暂无含可用模型的官方分组。')}</b>
        </div>
      )}
      {items.map((item) => (
        <article
          key={item.group_id}
          className={`gcard official-gcard${selected.has(item.group_id) ? 'in' : ''}`}
        >
          <div className='halo' />
          <div className='top'>
            <span className='src'>{t('官方')}</span>
            <div>
              <h3>{item.system_display_name}</h3>
              <p className='official-source'>{item.source_label}</p>
            </div>
            <div className='state'>
              <span
                className={`pub ${item.lifecycle_status === 'active' ? 'on' : 'off'}`}
              >
                {item.observing ? (
                  <CircleDashed size={12} />
                ) : (
                  <BadgeCheck size={12} />
                )}
                {item.observing
                  ? t('观测中')
                  : item.lifecycle_status === 'active'
                    ? t('可用')
                    : t('已暂停')}
              </span>
              <span className='sub2'>
                {item.metrics_available
                  ? t('{{count}} / 24H', { count: item.request_count })
                  : t('暂无请求')}
              </span>
            </div>
          </div>
          <div className='metrics official-metrics'>
            <div className='m'>
              <b>
                {item.multiplier}
                <span className='u'>×</span>
              </b>
              <span>{t('倍率')}</span>
            </div>
            <div
              className={
                item.metrics_available && item.success_rate >= 90
                  ? 'm good'
                  : item.metrics_available && item.success_rate < 75
                    ? 'm bad'
                    : 'm'
              }
            >
              <b>
                {item.metrics_available ? item.success_rate.toFixed(1) : '—'}
                <span className='u'>%</span>
              </b>
              <span>{t('24H 成功')}</span>
            </div>
            <div
              className={
                item.metrics_available && item.avg_ttft_ms > 600
                  ? 'm warn'
                  : 'm'
              }
            >
              <b>
                {item.metrics_available
                  ? (item.avg_ttft_ms / 1000).toFixed(2)
                  : '—'}
                <span className='u'>s</span>
              </b>
              <span>{t('P50')}</span>
            </div>
            <div className='m'>
              <b>
                {item.metrics_available ? item.cache_hit_rate.toFixed(0) : '—'}
                <span className='u'>%</span>
              </b>
              <span>{t('缓存命中')}</span>
            </div>
            <div className='m'>
              <b>{item.metrics_available ? item.request_count : '—'}</b>
              <span>{t('24H 请求')}</span>
            </div>
          </div>
          <div className='mline'>
            {item.models.slice(0, 5).map((model) => (
              <span className='mtag' key={model}>
                {model}
              </span>
            ))}
            {item.models.length > 5 && (
              <button
                className='mtag more'
                aria-expanded={expanded.has(item.group_id)}
                onClick={() =>
                  setExpanded((current) => {
                    const next = new Set(current)
                    if (next.has(item.group_id)) next.delete(item.group_id)
                    else next.add(item.group_id)
                    return next
                  })
                }
              >
                {expanded.has(item.group_id)
                  ? t('收起模型')
                  : t('全部 {{count}} 模型', { count: item.models.length })}
                <ChevronDown
                  size={11}
                  className={
                    expanded.has(item.group_id)
                      ? 'official-chevron-open'
                      : undefined
                  }
                />
              </button>
            )}
          </div>
          {expanded.has(item.group_id) && (
            <div className='official-model-list'>
              {item.models.slice(5).map((model) => (
                <span className='mtag' key={model}>
                  {model}
                </span>
              ))}
            </div>
          )}
          <div className='gact'>
            <button
              className='btn mini'
              disabled={
                !props.poolID ||
                pending ||
                target.isFetching ||
                target.isError ||
                selected.has(item.group_id)
              }
              onClick={() => void add(item)}
            >
              {selected.has(item.group_id)
                ? t('已在当前路由池')
                : t('加入当前路由池')}
            </button>
            {selected.has(item.group_id) && (
              <span className='inpool'>{t('已入池')}</span>
            )}
          </div>
        </article>
      ))}
      {!props.poolID && (
        <p className='official-pool-hint'>{t('请先创建或选择路由池。')}</p>
      )}
      {target.isError && props.poolID && (
        <button
          className='btn mini official-pool-retry'
          onClick={() => void target.refetch()}
        >
          {t('路由池加载失败，点击重试')}
        </button>
      )}
    </section>
  )
}
