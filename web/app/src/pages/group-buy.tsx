import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions } from '../lib/queries'
import { credits, date } from '../lib/format'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, PageHeader, Status } from '../components/ui'

type Group = Schema['MarketplaceGroup']

export default function GroupBuyPage() {
  const { t } = useTranslation()
  const [before, setBefore] = useState('')
  const [mine, setMine] = useState(false)
  const [selected, setSelected] = useState<Group['id'] | null>(null)
  const groups = useSuspenseQuery(
    resourceOptions(
      'groups',
      (signal) =>
        api
          .GET(mine ? '/api/group-buy/mine' : '/api/group-buy/list', {
            signal,
            params: { query: { before: before || undefined } },
          })
          .then((result) => unwrap(result)),
      [before, mine],
    ),
  ).data
  const orders = useSuspenseQuery(
    resourceOptions('orders', (signal) =>
      api.GET('/api/commerce/orders', { signal }).then((result) => unwrap(result)),
    ),
  ).data
  const plans = useSuspenseQuery(
    resourceOptions('plans', (signal) =>
      api.GET('/api/subscription/plans', { signal }).then((result) => unwrap(result)),
    ),
  ).data
  const queryClient = useQueryClient()
  const mutation = useMutation({
    mutationFn: (body: Schema['GroupCreateInput'] | Schema['GroupJoinInput']) =>
      'group_buy_id' in body
        ? api.POST('/api/group-buy/join', { body })
        : api.POST('/api/group-buy/create', { body }),
    onSuccess: () => {
      setSelected(null)
      void queryClient.invalidateQueries({ queryKey: ['groups'] })
    },
  })
  const paid =
    orders?.filter(
      (order) => order.state === 'paid' && order.kind === 'subscription' && order.group_buy_enabled,
    ) ?? []
  return (
    <>
      <PageHeader title="拼团" />
      <div className="filters">
        <Button
          variant="quiet"
          aria-pressed={!mine}
          onClick={() => {
            setMine(false)
            setBefore('')
          }}
        >
          全部拼团
        </Button>
        <Button
          variant="quiet"
          aria-pressed={mine}
          onClick={() => {
            setMine(true)
            setBefore('')
          }}
        >
          我的拼团
        </Button>
      </div>
      <ErrorMessage error={mutation.error} />
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          mutation.mutate({
            order_id: String(new FormData(event.currentTarget).get('order_id')),
            ...(selected ? { group_buy_id: selected } : {}),
          })
        }}
      >
        <label className="field" htmlFor="group-order">
          <span>{t('已支付的订阅订单')}</span>
          <select id="group-order" name="order_id" required>
            <option value="">{t('选择订单')}</option>
            {paid.map((order) => (
              <option value={String(order.id)} key={order.id}>
                {order.trade_no}
              </option>
            ))}
          </select>
        </label>
        <Button type="submit" disabled={mutation.isPending || !paid.length}>
          {t(selected ? '加入拼团' : '发起拼团')}
          {selected ? ` #${selected}` : ''}
        </Button>
        {selected && (
          <Button type="button" variant="quiet" onClick={() => setSelected(null)}>
            {t('取消')}
          </Button>
        )}
      </form>
      <DataTable
        rows={groups ?? []}
        rowKey={(row) => row.id}
        columns={[
          { label: '拼团', render: (row) => `#${row.id}` },
          {
            label: '套餐',
            render: (row) =>
              plans?.find((plan) => String(plan.id) === String(row.plan_id))?.name ?? row.plan_id,
          },
          { label: '人数', render: (row) => `${row.current_count} / ${row.target_count}` },
          { label: '奖励', render: (row) => credits(row.bonus_micro), numeric: true },
          { label: '状态', render: (row) => <Status value={row.status} /> },
          { label: '截止时间', render: (row) => date(row.expires_at) },
          {
            label: '操作',
            render: (row) => (
              <Button
                variant="quiet"
                disabled={row.status !== 'pending'}
                onClick={() => {
                  setSelected(row.id)
                  document.getElementById('group-order')?.focus()
                }}
              >
                {t('加入')}
              </Button>
            ),
          },
        ]}
      />
      <div className="filters section">
        <Button variant="quiet" disabled={!before} onClick={() => setBefore('')}>
          {t('返回最新')}
        </Button>
        <Button
          variant="quiet"
          disabled={(groups?.length ?? 0) < 30}
          onClick={() => setBefore(String(groups.at(-1)?.id ?? ''))}
        >
          {t('下一页')}
        </Button>
      </div>
    </>
  )
}
