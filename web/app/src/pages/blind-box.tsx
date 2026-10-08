import { useRef, useState } from 'react'
import { useMutation, useQuery, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage, PageHeader, confirmAction } from '../components/ui'
import { BoxHistory } from '../features/box-history'
import { BoxGuarantees } from '../features/box-guarantees'
import { BoxRewardList } from '../features/blind-box/reward-list'
import { RetainedBoxProps } from '../features/blind-box/retained-props'
import { BoxTransfers } from '../features/blind-box/transfers'
import { boxOperationIDs, enabledPools, rewardTotal } from '../features/blind-box/presentation'
import { BoxBatchOffers } from '../features/blind-box/batch-offers'
import { orderedLegacyInventory } from '../features/blind-box/batch-presentation'
import type { LegacyBoxInventory } from '../features/blind-box/batch-contract'
import '../features/blind-box/blind-box.css'

type Opened = Schema['MarketplaceOpenRecord']

export default function BlindBoxPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data } = useSuspenseQuery(
    resourceOptions('boxes', (signal) => api.GET('/api/blind-box/self', { signal }).then(unwrap)),
  )
  const operations = useRef(boxOperationIDs())
  const inventoryQuery = useQuery(
    resourceOptions<Schema['MarketplaceOverview'] & { inventory?: LegacyBoxInventory[] }>(
      'box-inventory',
      (signal) => api.GET('/api/blind-box/inventory/overview', { signal }).then(unwrap),
    ),
  )
  const [inventoryPoolID, setInventoryPoolID] = useState('')
  const [selectedID, setSelectedID] = useState('')
  const [opened, setOpened] = useState<Opened[]>([])
  const [notice, setNotice] = useState('')
  const [confirming, setConfirming] = useState(false)
  const pools = enabledPools(data.pools)
  const selected = pools.find((pool) => String(pool.id) === selectedID) ?? pools[0]
  const inventory = orderedLegacyInventory(inventoryQuery.data?.inventory ?? [])
  const inventoryPools = inventory
  const inventoryKey = (item: LegacyBoxInventory) => `${item.pool_id}:${item.draw_current_pool}`
  const selectedInventory =
    inventoryPools.find((item) => inventoryKey(item) === inventoryPoolID) ?? inventoryPools[0]
  const hasInventory = BigInt(inventoryQuery.data?.available_count ?? data.available_count) > 0n
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['boxes'] })
    void queryClient.invalidateQueries({ queryKey: ['wallet'] })
    void queryClient.invalidateQueries({ queryKey: ['box-history'] })
    void queryClient.invalidateQueries({ queryKey: ['box-inventory'] })
  }
  const begin = () => setNotice('')
  const buy = useMutation({
    mutationFn: (pool: Schema['MarketplacePool']['id']) =>
      api
        .POST('/api/blind-box/inventory/purchase', {
          body: {
            request_id: operations.current.forPayload(`purchase:${pool}`),
            pool_id: pool,
            count: 1,
          },
        })
        .then(unwrap),
    onMutate: begin,
    onSuccess: (result, pool) => {
      operations.current.complete(`purchase:${pool}`)
      setNotice(`${t('购买成功，已加入未开启库存')} · ${credits(result.total_micro)}`)
      refresh()
    },
  })
  const open = useMutation({
    mutationFn: (item?: LegacyBoxInventory) =>
      api
        .POST('/api/blind-box/inventory/open', {
          body: {
            request_id: operations.current.forPayload(`open:${item ? inventoryKey(item) : ''}`),
            pool_id: item?.pool_id,
            draw_current_pool: item?.draw_current_pool,
            count: 1,
          },
        })
        .then(unwrap),
    onMutate: () => {
      begin()
      setOpened([])
    },
    onSuccess: (result, item) => {
      operations.current.complete(`open:${item ? inventoryKey(item) : ''}`)
      setOpened(result)
      refresh()
    },
  })
  const gift = useMutation({
    mutationFn: (recipient: string) =>
      api.POST('/api/blind-box/inventory/gift', {
        body: {
          request_id: operations.current.forPayload(`gift:${recipient}`),
          recipient_id: recipient,
          count: 1,
        },
      }),
    onMutate: begin,
    onSuccess: (_result, recipient) => {
      operations.current.complete(`gift:${recipient}`)
      setNotice(t('已赠送'))
      refresh()
    },
  })
  const giftProp = useMutation({
    mutationFn: (input: { id: string; recipient: string }) =>
      api.POST('/api/blind-box/props/{id}/gift', {
        params: { path: { id: input.id } },
        body: {
          request_id: operations.current.forPayload(`prop-gift:${input.id}:${input.recipient}`),
          recipient_id: input.recipient,
        },
      }),
    onMutate: begin,
    onSuccess: (_result, input) => {
      operations.current.complete(`prop-gift:${input.id}:${input.recipient}`)
      setNotice(t('已赠送'))
      refresh()
    },
  })
  const prop = useMutation({
    mutationFn: (input: { id: Schema['MarketplaceProp']['id']; action: 'pause' | 'use' }) =>
      api.POST(
        input.action === 'pause'
          ? '/api/blind-box/props/{id}/pause'
          : '/api/blind-box/props/{id}/use',
        { params: { path: { id: String(input.id) } } },
      ),
    onMutate: begin,
    onSuccess: (_result, input) => {
      setNotice(t(input.action === 'pause' ? '已暂停' : '已使用'))
      refresh()
    },
  })
  const convert = useMutation({
    mutationFn: (id: Schema['MarketplaceProp']['id']) =>
      api.POST('/api/blind-box/props/{id}/convert', {
        params: { path: { id } },
        body: { target_type: 'topup_discount_90' },
      }),
    onMutate: begin,
    onSuccess: () => {
      setNotice(t('已转换'))
      refresh()
    },
  })
  const pending =
    buy.isPending ||
    open.isPending ||
    gift.isPending ||
    giftProp.isPending ||
    prop.isPending ||
    convert.isPending ||
    confirming
  const errors = [
    buy.error,
    open.error,
    gift.error,
    giftProp.error,
    prop.error,
    convert.error,
  ].filter(Boolean)
  const resetErrors = () => {
    buy.reset()
    open.reset()
    gift.reset()
    giftProp.reset()
    prop.reset()
    convert.reset()
  }
  const rewards = selected?.rewards ?? []
  const totalWeight = rewardTotal(rewards)
  const validWeights = totalWeight !== undefined
  return (
    <div className="blind-box-page">
      <PageHeader
        title="盲盒"
        description="公开每批奖品、剩余概率和消费限制。旧版盲盒、道具和保底继续按原承诺使用。"
      />
      <BoxBatchOffers pending={pending} />
      <h2>{t('旧版盲盒与库存')}</h2>
      <ErrorMessage error={inventoryQuery.error} />
      {inventoryQuery.isError && (
        <Button variant="quiet" onClick={() => void inventoryQuery.refetch()}>
          {t('重试')}
        </Button>
      )}
      <section className="box-inventory section" aria-labelledby="box-inventory-heading">
        <div>
          <h2 id="box-inventory-heading">{t('我的盲盒')}</h2>
          <p className="box-inventory-count">
            <strong>{String(inventoryQuery.data?.available_count ?? data.available_count)}</strong>{' '}
            <span>{t('未开启')}</span>
          </p>
          {inventoryPools.length > 0 && (
            <div className="field">
              <label htmlFor="legacy-box-inventory">{t('选择旧库存奖池')}</label>
              <select
                id="legacy-box-inventory"
                disabled={pending}
                value={selectedInventory ? inventoryKey(selectedInventory) : ''}
                onChange={(event) => setInventoryPoolID(event.target.value)}
              >
                {inventoryPools.map((item) => (
                  <option key={inventoryKey(item)} value={inventoryKey(item)}>
                    {item.pool_name} · #{String(item.pool_id)} ·{' '}
                    {t(item.draw_current_pool ? '原动态规则' : '原冻结权益')}
                  </option>
                ))}
              </select>
            </div>
          )}
          {inventory
            .filter(
              (item) => selectedInventory && inventoryKey(item) === inventoryKey(selectedInventory),
            )
            .map((item) => (
              <p
                className="muted"
                key={`${item.pool_id}:${item.draw_current_pool}:${item.expires_at}`}
              >
                {t(item.draw_current_pool ? '原动态规则' : '购买时冻结规则')} ·{' '}
                {String(item.available_count)} {t('个')} · {t('到期时间')} {date(item.expires_at)}
              </p>
            ))}
          {inventory.length > 0 && (
            <p className="muted">
              {t('开启所选奖池最早到期的库存，动态与冻结库存各按原规则履约。')}
            </p>
          )}
          <p className="muted">
            {t('开启已有盲盒不会再次扣除购买费用，奖励按该盲盒对应规则发放。')}
          </p>
        </div>
        <Button
          disabled={pending || !hasInventory || inventoryQuery.isPending || inventoryQuery.isError}
          loading={open.isPending}
          onClick={() => {
            resetErrors()
            open.mutate(selectedInventory)
          }}
        >
          {t(open.isPending ? '正在开启' : '开启一个')}
        </Button>
      </section>
      {errors.map((error, index) => (
        <ErrorMessage key={index} error={error} />
      ))}
      {!!errors.length && (
        <p className="muted">
          {t('操作失败时可在当前页面重试；同一操作会复用请求编号，避免重复扣费或发奖。')}
        </p>
      )}
      {notice && (
        <p role="status" className="notice">
          {notice}
        </p>
      )}
      {opened.length > 0 && (
        <section
          className="box-open-result notice"
          role="status"
          aria-live="polite"
          aria-labelledby="box-result-heading"
        >
          <h2 id="box-result-heading">{t('本次开启结果')}</h2>
          {opened.map((item) => (
            <p key={String(item.id)}>
              <strong>{item.reward.title}</strong>
              {item.reward.kind === 'credits' && <> · {credits(item.reward.amount_micro)}</>}
            </p>
          ))}
          <p className="muted">{t('额度已计入钱包；道具和套餐权益可在对应页面查看。')}</p>
        </section>
      )}
      <section className="section box-purchase" aria-labelledby="box-purchase-heading">
        <div className="box-section-heading">
          <div>
            <h2 id="box-purchase-heading">{t('购买盲盒')}</h2>
            <p className="muted">{t('奖励随机，可能低于购买价格；请按预算选择。')}</p>
          </div>
          {pools.length > 1 && (
            <div className="field">
              <label htmlFor="box-pool">{t('选择奖池')}</label>
              <select
                id="box-pool"
                value={String(selected?.id ?? '')}
                disabled={pending}
                onChange={(event) => setSelectedID(event.target.value)}
              >
                {pools.map((pool) => (
                  <option key={String(pool.id)} value={String(pool.id)}>
                    {pool.name}
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>
        {selected ? (
          <div className="box-pool-layout">
            <div className="box-pool-main">
              <h3>{selected.name}</h3>
              <h4>{t('基础奖池占比')}</h4>
              <p className="muted">
                {t(
                  '以下占比仅代表基础奖池权重；首购、保底、套餐奖励与零时卡分支可能改变本次结果。',
                )}
              </p>
              <BoxRewardList rewards={rewards.slice(0, 4)} total={totalWeight} />
              {rewards.length > 4 && (
                <details>
                  <summary>{t('查看其余基础奖励')}</summary>
                  <BoxRewardList rewards={rewards.slice(4)} total={totalWeight} />
                </details>
              )}
              {!validWeights && <p className="notice">{t('奖池概率配置暂不可用，请稍后再试。')}</p>}
              <BoxGuarantees pool={selected} state={data.pity_states?.[String(selected.id)]} />
              <p className="box-rule-note muted">
                {t('这里展示当前在售奖池；历史未开启盲盒可能使用购买时冻结的规则或原有动态规则。')}
              </p>
            </div>
            <aside className="box-pool-order" aria-label={t('购买信息')}>
              <p className="muted">{t('单个价格')}</p>
              <p className="box-price">{credits(selected.price_micro)}</p>
              <dl className="box-limits">
                <div>
                  <dt>{t('每日限购')}</dt>
                  <dd>{selected.daily_limit || t('不限')}</dd>
                </div>
                <div>
                  <dt>{t('每月限购')}</dt>
                  <dd>{selected.monthly_limit || t('不限')}</dd>
                </div>
                <div>
                  <dt>{t('每日开启上限')}</dt>
                  <dd>{selected.daily_open_limit || t('不限')}</dd>
                </div>
              </dl>
              <p className="muted">{t('限购和开启次数以服务端校验为准。')}</p>
              <Button
                disabled={(pending && !confirming) || !validWeights}
                loading={buy.isPending}
                onClick={async (event) => {
                  if (confirming) return
                  const trigger = event.currentTarget
                  let accepted = false
                  setConfirming(true)
                  try {
                    accepted = await confirmAction({
                      title: `${t('购买')} ${selected.name} · ${credits(selected.price_micro)}?`,
                      description: t('购买后加入未开启库存。奖励随机，可能低于购买价格。'),
                    })
                    if (accepted) {
                      resetErrors()
                      buy.mutate(selected.id)
                    }
                  } finally {
                    setConfirming(false)
                    if (!accepted) requestAnimationFrame(() => trigger.focus())
                  }
                }}
              >
                {t(buy.isPending ? '正在购买' : '购买一个')}
              </Button>
            </aside>
          </div>
        ) : (
          <div className="empty-state">
            <p>{t('暂无可购买盲盒')}</p>
            <p>{t('已有库存仍可按原规则开启。')}</p>
          </div>
        )}
      </section>
      {data.zero_hour &&
        (Number(data.zero_hour.points) > 0 ||
          data.zero_hour.active ||
          data.zero_hour.max_probability > 0) && (
          <details className="section box-legacy-rules">
            <summary>{t('零时卡进度')}</summary>
            <p>
              {t('抽取进度')} {String(data.zero_hour.points)} / {String(data.zero_hour.point_cap)}
              {t('，当前概率')} {(data.zero_hour.current_probability * 100).toFixed(3)}
              {t('%，最高')} {(data.zero_hour.max_probability * 100).toFixed(3)}%。
            </p>
            {data.zero_hour.active && (
              <p>
                {t('当前已生效，到期时间')} {date(Number(data.zero_hour.active_until) * 1000)}。
              </p>
            )}
            <p className="muted">{t('零时卡按既有独立规则运行，不保证命中或盈利。')}</p>
          </details>
        )}
      <RetainedBoxProps
        items={data.props ?? []}
        pending={pending}
        onAction={(input) => {
          resetErrors()
          prop.mutate(input)
        }}
        onConvert={(id) => {
          resetErrors()
          convert.mutate(id)
        }}
      />
      <BoxHistory />
      <BoxTransfers
        items={data.props ?? []}
        available={hasInventory}
        pending={pending}
        onGiftBox={(recipient) => {
          resetErrors()
          gift.mutate(recipient)
        }}
        onGiftProp={(input) => {
          resetErrors()
          giftProp.mutate(input)
        }}
      />
    </div>
  )
}
