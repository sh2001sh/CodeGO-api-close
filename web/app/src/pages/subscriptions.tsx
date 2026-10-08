import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { sessionOptions } from '../lib/queries'
import { credits } from '../lib/format'
import { paymentAmount, type Plan } from '../lib/commerce'
import { subscriptionPolicyLabel } from '../lib/subscription-policy'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, PageHeader, Status } from '../components/ui'
import { PlanForm } from '../features/plan-form'
import { SubscriptionAdmin } from '../features/commerce/subscription-admin'
import { SubscriptionRedesignAdmin } from '../features/commerce/subscription-redesign-admin'
import { ReferralPolicyAdmin } from '../features/commerce/referral-policy-admin'

export const adminPlansOptions = () =>
  resourceOptions('admin-plans', (signal) =>
    api.GET('/api/subscription/admin/plans', { signal }).then((result) => unwrap(result)),
  )

export default function SubscriptionsPage() {
  const { t } = useTranslation()
  const plans = useSuspenseQuery(adminPlansOptions()).data
  const user = useSuspenseQuery(sessionOptions()).data
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
            {t('新增套餐')}
          </Button>
        }
      />
      <ErrorMessage error={save.error ?? status.error ?? remove.error} />
      {removing && (
        <div className="form-panel">
          <p className="full-width">
            {t('确认停用“{name}”？套餐和历史记录将保留，已发行兑换码仍可兑现。', {
              name: removing.name,
            })}
          </p>
          <Button
            variant="danger"
            disabled={remove.isPending}
            onClick={() => remove.mutate(removing.id)}
          >
            {t('确认停用套餐')}
          </Button>
          <Button variant="quiet" disabled={remove.isPending} onClick={() => setRemoving(null)}>
            {t('取消')}
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
          { label: '规则', render: (row) => t(subscriptionPolicyLabel(row)) },
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
                ? String(row.custom_seconds) + t(' 秒')
                : String(row.duration_value) +
                  ' ' +
                  t(
                    { day: '天', hour: '小时', month: '自然月', year: '自然年' }[
                      row.duration_unit
                    ] ?? row.duration_unit,
                  ),
          },
          {
            label: '状态',
            render: (row) => <Status value={row.enabled ? 'active' : 'disabled'} />,
          },
          {
            label: '拼团',
            render: (row) =>
              row.group_buy_enabled
                ? String(row.group_buy_target) +
                  t(' 人 · 奖励 ') +
                  String(credits(row.group_buy_bonus))
                : t('关闭'),
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
                  {t('编辑')}
                </Button>
                <Button
                  variant="quiet"
                  disabled={status.isPending || !!editing || !!removing}
                  onClick={() => status.mutate({ id: row.id, enabled: !row.enabled })}
                >
                  {row.enabled ? t('下架') : t('上架')}
                </Button>
                <Button
                  variant="danger"
                  disabled={status.isPending || !!editing || !!removing}
                  onClick={() => {
                    remove.reset()
                    setRemoving(row)
                  }}
                >
                  {t('停用套餐')}
                </Button>
              </div>
            ),
          },
        ]}
      />
      <SubscriptionAdmin plans={plans ?? []} />
      {user.role === 'root' && <SubscriptionRedesignAdmin plans={plans ?? []} />}
      {user.role === 'root' && <ReferralPolicyAdmin />}
    </>
  )
}
