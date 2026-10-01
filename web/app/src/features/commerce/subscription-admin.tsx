import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { resourceOptions } from '../../lib/queries'
import { credits, date } from '../../lib/format'
import { DataTable } from '../../components/data-table'
import { Button, ErrorMessage, Field, Loading, Status } from '../../components/ui'
import { errorFrom, positiveID } from './amounts'
import { SubscriptionBind } from './subscription-bind'
import { SubscriptionEdit } from './subscription-edit'

export const adminUserSubscriptionsOptions = (userID: string) => ({
  ...resourceOptions(
    'admin-user-subscriptions',
    (signal) =>
      api
        .GET('/api/subscription/admin/users/{id}/subscriptions', {
          signal,
          params: { path: { id: userID } },
        })
        .then(unwrap),
    [userID],
  ),
  enabled: !!userID,
})

type Action = {
  kind: 'invalidate' | 'reset' | 'delete'
  subscription: Schema['Subscription']
  requestID: string
}

export function SubscriptionAdmin(props: { plans: Schema['Plan'][] }) {
  const [userID, setUserID] = useState('')
  const [error, setError] = useState<Error | null>(null)
  const [action, setAction] = useState<Action | null>(null)
  const [editing, setEditing] = useState<Schema['Subscription'] | null>(null)
  const client = useQueryClient()
  const list = useQuery(adminUserSubscriptionsOptions(userID))
  const mutation = useMutation({
    mutationFn: (operation: Action) => {
      const params = { path: { id: operation.subscription.id } }
      if (operation.kind === 'reset')
        return api.POST('/api/subscription/admin/user_subscriptions/{id}/reset', {
          params,
          body: { request_id: operation.requestID },
        })
      if (operation.kind === 'invalidate')
        return api.POST('/api/subscription/admin/user_subscriptions/{id}/invalidate', { params })
      return api.DELETE('/api/subscription/admin/user_subscriptions/{id}', { params })
    },
    onSuccess: () => {
      setAction(null)
      void client.invalidateQueries({ queryKey: ['admin-user-subscriptions'] })
    },
  })
  const open = (subscription: Schema['Subscription'], kind: Action['kind']) => {
    mutation.reset()
    setEditing(null)
    setAction({ subscription, kind, requestID: crypto.randomUUID() })
  }
  const operationLabel =
    action?.kind === 'reset'
      ? '重置周期额度'
      : action?.kind === 'invalidate'
        ? '使订阅失效'
        : '删除订阅'
  return (
    <section className="section">
      <h2>用户订阅管理</h2>
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          setError(null)
          try {
            const id = positiveID(
              String(new FormData(event.currentTarget).get('subscription-user-id') ?? '').trim(),
            )
            setUserID(id)
            setEditing(null)
            setAction(null)
            mutation.reset()
          } catch (cause) {
            setError(errorFrom(cause))
          }
        }}
      >
        <Field name="subscription-user-id" label="用户编号" required placeholder="输入用户编号" />
        <Button type="submit" disabled={list.isFetching}>
          查询用户订阅
        </Button>
      </form>
      <ErrorMessage error={error ?? list.error ?? mutation.error} />
      {list.isFetching && <Loading />}
      {userID && (
        <>
          <SubscriptionBind key={userID} userID={userID} plans={props.plans} />
          {editing && (
            <SubscriptionEdit
              key={String(editing.id)}
              subscription={editing}
              onClose={() => setEditing(null)}
            />
          )}
          {action && (
            <div className="form-panel">
              <p className="full-width">
                确认{operationLabel}（编号 {String(action.subscription.id)}）？
                {action.kind === 'reset'
                  ? '周期用量会归零，服务器将根据套餐规则重新发放可用额度。'
                  : '剩余额度将清零，正在结算的消费会先完成。'}
              </p>
              <Button
                variant={action.kind === 'reset' ? 'primary' : 'danger'}
                disabled={mutation.isPending}
                onClick={() => mutation.mutate(action)}
              >
                {mutation.isPending
                  ? '处理中…'
                  : mutation.isError
                    ? `重试${operationLabel}`
                    : `确认${operationLabel}`}
              </Button>
              {!mutation.isError && (
                <Button
                  variant="quiet"
                  disabled={mutation.isPending}
                  onClick={() => setAction(null)}
                >
                  取消
                </Button>
              )}
            </div>
          )}
          <DataTable
            rows={list.data ?? []}
            rowKey={(row) => row.id}
            empty="该用户暂无订阅，可在上方分配套餐。"
            columns={[
              { label: '订阅编号', render: (row) => String(row.id) },
              {
                label: '套餐',
                render: (row) =>
                  props.plans.find((plan) => String(plan.id) === String(row.plan_id))?.name ??
                  String(row.plan_id),
              },
              { label: '余额', render: (row) => credits(row.balance), numeric: true },
              {
                label: '总额度 / 已使用',
                render: (row) => `${credits(row.total_credits)} / ${credits(row.used_credits)}`,
              },
              {
                label: '周期额度 / 已使用',
                render: (row) => `${credits(row.period_credits)} / ${credits(row.period_used)}`,
              },
              { label: '状态', render: (row) => <Status value={row.state} /> },
              { label: '到期时间', render: (row) => date(row.expires_at) },
              { label: '下次重置', render: (row) => date(row.next_reset_at) },
              {
                label: '操作',
                render: (row) => (
                  <div className="row-actions">
                    <Button
                      variant="quiet"
                      disabled={!!action || !!editing}
                      onClick={() => setEditing(row)}
                    >
                      编辑
                    </Button>
                    <Button
                      variant="quiet"
                      disabled={!!action || !!editing || row.state !== 'active'}
                      onClick={() => open(row, 'reset')}
                    >
                      重置额度
                    </Button>
                    <Button
                      variant="danger"
                      disabled={!!action || !!editing || row.state !== 'active'}
                      onClick={() => open(row, 'invalidate')}
                    >
                      使其失效
                    </Button>
                    <Button
                      variant="danger"
                      disabled={!!action || !!editing}
                      onClick={() => open(row, 'delete')}
                    >
                      删除
                    </Button>
                  </div>
                ),
              },
            ]}
          />
        </>
      )}
    </section>
  )
}
