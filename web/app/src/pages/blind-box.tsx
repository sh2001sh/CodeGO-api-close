import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, PageHeader, Status } from '../components/ui'
import { BoxHistory } from '../features/box-history'
import { GiftProp } from '../features/prop-gift'
import { BoxGuarantees } from '../features/box-guarantees'

type Opened = Schema['MarketplaceOpenRecord']

export default function BlindBoxPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data } = useSuspenseQuery(
    resourceOptions('boxes', (signal) =>
      api.GET('/api/blind-box/self', { signal }).then((result) => unwrap(result)),
    ),
  )
  const [requestID, setRequestID] = useState(() => crypto.randomUUID())
  const [opened, setOpened] = useState<Opened[]>([])
  const refresh = () => {
    setRequestID(crypto.randomUUID())
    void queryClient.invalidateQueries({ queryKey: ['boxes'] })
    void queryClient.invalidateQueries({ queryKey: ['wallet'] })
    void queryClient.invalidateQueries({ queryKey: ['box-history'] })
  }
  const buy = useMutation({
    mutationFn: (pool: Schema['MarketplacePool']['id']) =>
      api.POST('/api/blind-box/inventory/purchase', {
        body: {
          request_id: requestID,
          pool_id: pool,
          count: 1,
        },
      }),
    onSuccess: refresh,
  })
  const open = useMutation({
    mutationFn: () =>
      api
        .POST('/api/blind-box/inventory/open', { body: { request_id: requestID, count: 1 } })
        .then((result) => unwrap(result)),
    onSuccess: (result) => {
      setOpened(result)
      refresh()
    },
  })
  const gift = useMutation({
    mutationFn: (recipient: string) =>
      api.POST('/api/blind-box/inventory/gift', {
        body: {
          request_id: requestID,
          recipient_id: recipient,
          count: 1,
        },
      }),
    onSuccess: refresh,
  })
  const prop = useMutation({
    mutationFn: (input: { id: Schema['MarketplaceProp']['id']; action: 'pause' | 'use' }) =>
      api.POST(
        input.action === 'pause'
          ? '/api/blind-box/props/{id}/pause'
          : '/api/blind-box/props/{id}/use',
        { params: { path: { id: String(input.id) } } },
      ),
    onSuccess: refresh,
  })
  const convert = useMutation({
    mutationFn: (id: Schema['MarketplaceProp']['id']) =>
      api.POST('/api/blind-box/props/{id}/convert', {
        params: { path: { id } },
        body: { target_type: 'topup_discount_90' },
      }),
    onSuccess: refresh,
  })
  const pending =
    buy.isPending || open.isPending || gift.isPending || prop.isPending || convert.isPending
  return (
    <>
      <PageHeader title="盲盒" />
      <ErrorMessage error={buy.error ?? open.error ?? gift.error ?? prop.error ?? convert.error} />
      <dl className="balance-ledger">
        <div>
          <dt>{t('未开启')}</dt>
          <dd>{data.available_count}</dd>
        </div>
      </dl>
      {data.zero_hour && (
        <details className="section">
          <summary>零时卡进度</summary>
          <p>
            抽取进度 {data.zero_hour.points} / {data.zero_hour.point_cap}，当前概率{' '}
            {(data.zero_hour.current_probability * 100).toFixed(3)}%，最高{' '}
            {(data.zero_hour.max_probability * 100).toFixed(3)}%。
          </p>
          {data.zero_hour.active && (
            <p>当前已生效，到期时间 {date(Number(data.zero_hour.active_until) * 1000)}。</p>
          )}
        </details>
      )}
      <div className="filters section">
        <Button
          disabled={pending || BigInt(data.available_count) < 1n}
          onClick={() => open.mutate()}
        >
          {t('开启一个')}
        </Button>
      </div>
      {opened.length > 0 && (
        <section className="notice" aria-live="polite">
          {opened.map((item) => (
            <div key={item.id}>
              {item.reward.title}{' '}
              {item.reward.kind === 'credits' ? credits(item.reward.amount_micro) : ''}
            </div>
          ))}
        </section>
      )}
      <section className="section">
        <h2>{t('购买盲盒')}</h2>
        <div className="catalog-grid">
          {data.pools
            ?.filter((pool) => pool.enabled)
            .map((pool) => (
              <article className="product" key={pool.id}>
                <h3>{pool.name}</h3>
                <BoxGuarantees pool={pool} state={data.pity_states?.[String(pool.id)]} />
                <dl>
                  <div>
                    <dt>{t('价格')}</dt>
                    <dd>{credits(pool.price_micro)}</dd>
                  </div>
                  <div>
                    <dt>{t('每日限购')}</dt>
                    <dd>{pool.daily_limit || t('不限')}</dd>
                  </div>
                  <div>
                    <dt>每月限购</dt>
                    <dd>{pool.monthly_limit || t('不限')}</dd>
                  </div>
                  <div>
                    <dt>每日开启上限</dt>
                    <dd>{pool.daily_open_limit || t('不限')}</dd>
                  </div>
                </dl>
                <details>
                  <summary>{t('奖励概率')}</summary>
                  {pool.standard_policy?.enabled && (
                    <p className="muted">
                      以下为基础奖池权重；首购、保底和订阅奖励分支会影响实际结果。
                    </p>
                  )}
                  <DataTable
                    rows={pool.rewards ?? []}
                    rowKey={(row) => `${row.kind}-${row.title}`}
                    columns={[
                      { label: '奖励', render: (row) => row.title },
                      {
                        label: '额度',
                        render: (row) =>
                          row.kind === 'credits'
                            ? BigInt(row.minimum_micro ?? 0) > 0n
                              ? `${credits(row.minimum_micro)} ～ ${credits(row.maximum_micro)}`
                              : credits(row.amount_micro)
                            : '—',
                      },
                      {
                        label: '概率',
                        render: (row) =>
                          `${((Number(row.weight) / (pool.rewards ?? []).reduce((total, reward) => total + Number(reward.weight), 0)) * 100).toFixed(2)}%`,
                        numeric: true,
                      },
                    ]}
                  />
                </details>
                <Button
                  disabled={pending}
                  onClick={() => {
                    if (window.confirm(`${t('购买')} ${pool.name} · ${credits(pool.price_micro)}?`))
                      buy.mutate(pool.id)
                  }}
                >
                  {t('购买')}
                </Button>
              </article>
            ))}
        </div>
        {!data.pools?.length && <div className="empty-state">{t('暂无可购买盲盒')}</div>}
      </section>
      <section className="section">
        <h2>{t('道具')}</h2>
        <DataTable
          rows={data.props ?? []}
          rowKey={(row) => row.id}
          columns={[
            { label: '名称', render: (row) => row.title },
            { label: '状态', render: (row) => <Status value={row.status} /> },
            {
              label: '倍率',
              render: (row) => Number(row.multiplier_ppm) / 1_000_000,
              numeric: true,
            },
            {
              label: '剩余小时',
              render: (row) => (Number(row.remaining_seconds) / 3600).toFixed(1),
              numeric: true,
            },
            { label: '到期时间', render: (row) => date(row.expires_at) },
            {
              label: '操作',
              render: (row) => {
                if (row.kind === 'topup_discount' || row.kind === 'subscription_discount')
                  return (
                    <div>
                      <span>
                        {row.kind === 'topup_discount' ? '充值时自动使用' : '购买套餐时自动使用'}
                      </span>
                      {row.status === 'available' && row.prop_type !== 'topup_discount_90' && (
                        <Button
                          variant="quiet"
                          disabled={pending}
                          onClick={() => {
                            if (window.confirm('将此道具转换为九折充值卡？')) convert.mutate(row.id)
                          }}
                        >
                          转换为九折充值卡
                        </Button>
                      )}
                    </div>
                  )
                if (row.kind === 'extra_draw') return <span>开启盲盒时自动使用</span>
                const canUse = row.status === 'available' || row.status === 'paused'
                const canPause = row.kind === 'multiplier' && row.status === 'active'
                return (
                  <Button
                    variant="quiet"
                    disabled={pending || (!canUse && !canPause)}
                    onClick={() => prop.mutate({ id: row.id, action: canPause ? 'pause' : 'use' })}
                  >
                    {t(canPause ? '暂停' : '使用')}
                  </Button>
                )
              },
            },
          ]}
        />
      </section>
      <section className="section">
        <h2>{t('赠送盲盒')}</h2>
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            gift.mutate(String(new FormData(event.currentTarget).get('recipient')))
          }}
        >
          <Field name="recipient" label="收件人用户 ID" type="number" min={1} required />
          <Button disabled={pending || BigInt(data.available_count) < 1n} type="submit">
            {t('赠送一个')}
          </Button>
        </form>
      </section>
      <GiftProp items={data.props ?? []} />
      <BoxHistory />
    </>
  )
}
