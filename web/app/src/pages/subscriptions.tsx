import { useState } from 'react'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { credits } from '../lib/format'
import { paymentAmount, type Plan } from '../lib/commerce'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, PageHeader, Status } from '../components/ui'
import { PlanForm } from '../features/plan-form'
import { SubscriptionAdmin } from '../features/commerce/subscription-admin'

export const adminPlansOptions = () =>
  resourceOptions('admin-plans', (signal) =>
    api.GET('/api/subscription/admin/plans', { signal }).then((result) => unwrap(result)),
  )

export default function SubscriptionsPage() {
  const plans = useSuspenseQuery(adminPlansOptions()).data
  const [editing, setEditing] = useState<Plan | 'new' | null>(null)
  const [removing, setRemoving] = useState<Plan | null>(null)
  const queryClient = useQueryClient()
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['admin-plans'] })
    void queryClient.invalidateQueries({ queryKey: ['plans'] })
  }
  const status = useMutation({
    mutationFn: (plan: Pick<Plan, 'id' | 'enabled'>) =>
      api.PATCH('/api/subscription/admin/plans/{id}', {
        params: { path: { id: plan.id } },
        body: { enabled: plan.enabled },
      }),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: Plan['id']) =>
      api.DELETE('/api/subscription/admin/plans/{id}', { params: { path: { id } } }),
    onSuccess: () => {
      setRemoving(null)
      refresh()
    },
  })
  const save = useMutation({
    mutationFn: (body: Plan) =>
      body.id
        ? api
            .PUT('/api/subscription/admin/plans/{id}', { params: { path: { id: body.id } }, body })
            .then((result) => unwrap(result))
        : api.POST('/api/subscription/admin/plans', { body }).then((result) => unwrap(result)),
    onSuccess: () => {
      setEditing(null)
      refresh()
    },
  })
  return (
    <>
      <PageHeader
        title="套餐管理"
        action={
          <Button
            onClick={() => {
              save.reset()
              setEditing('new')
            }}
          >
            新增套餐
          </Button>
        }
      />
      <ErrorMessage error={save.error ?? status.error ?? remove.error} />
      {removing && (
        <div className="form-panel">
          <p className="full-width">
            确认删除“{removing.name}
            ”？已有订单或订阅的套餐会下架并保留历史记录；尚未使用的套餐会删除。
          </p>
          <Button
            variant="danger"
            disabled={remove.isPending}
            onClick={() => remove.mutate(removing.id)}
          >
            确认删除套餐
          </Button>
          <Button variant="quiet" disabled={remove.isPending} onClick={() => setRemoving(null)}>
            取消
          </Button>
        </div>
      )}
      {editing && (
        <PlanForm
          key={editing === 'new' ? 'new' : String(editing.id)}
          plan={editing === 'new' ? undefined : editing}
          pending={save.isPending}
          onSave={(body) => save.mutate(body)}
          onCancel={() => setEditing(null)}
        />
      )}
      <DataTable
        rows={plans ?? []}
        rowKey={(row) => row.id}
        empty="暂无套餐。新增套餐后用户可在钱包购买。"
        columns={[
          { label: '套餐名称', render: (row) => row.name },
          { label: '额度', render: (row) => credits(row.credits), numeric: true },
          {
            label: '价格',
            render: (row) => paymentAmount(row.price_minor, row.currency),
            numeric: true,
          },
          {
            label: '有效期',
            render: (row) =>
              row.duration_unit === 'custom'
                ? `${row.custom_seconds} 秒`
                : `${row.duration_value} ${{ day: '天', hour: '小时', month: '自然月', year: '自然年' }[row.duration_unit] ?? row.duration_unit}`,
          },
          {
            label: '状态',
            render: (row) => <Status value={row.enabled ? 'active' : 'disabled'} />,
          },
          {
            label: '拼团',
            render: (row) =>
              row.group_buy_enabled
                ? `${row.group_buy_target} 人 · 奖励 ${credits(row.group_buy_bonus)}`
                : '关闭',
          },
          {
            label: '操作',
            render: (row) => (
              <div className="row-actions">
                <Button
                  variant="quiet"
                  disabled={status.isPending || !!removing}
                  onClick={() => {
                    save.reset()
                    setEditing(row)
                  }}
                >
                  编辑
                </Button>
                <Button
                  variant="quiet"
                  disabled={status.isPending || !!editing || !!removing}
                  onClick={() => status.mutate({ id: row.id, enabled: !row.enabled })}
                >
                  {row.enabled ? '下架' : '上架'}
                </Button>
                <Button
                  variant="danger"
                  disabled={status.isPending || !!editing || !!removing}
                  onClick={() => {
                    remove.reset()
                    setRemoving(row)
                  }}
                >
                  删除套餐
                </Button>
              </div>
            ),
          },
        ]}
      />
      <SubscriptionAdmin plans={plans ?? []} />
    </>
  )
}
