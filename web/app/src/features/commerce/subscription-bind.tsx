import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { Button, ErrorMessage } from '../../components/ui'

export function SubscriptionBind(props: { userID: string; plans: Schema['Plan'][] }) {
  const client = useQueryClient()
  const [body, setBody] = useState<Schema['BindSubscriptionInput'] | null>(null)
  const bind = useMutation({
    mutationFn: (input: Schema['BindSubscriptionInput']) =>
      api
        .POST('/api/subscription/admin/users/{id}/subscriptions', {
          params: { path: { id: props.userID } },
          body: input,
        })
        .then(unwrap),
    onSuccess: () => {
      setBody(null)
      void client.invalidateQueries({ queryKey: ['admin-user-subscriptions'] })
    },
  })
  return (
    <section className="section">
      <h3>向用户 {props.userID} 分配套餐</h3>
      <p className="muted">此操作直接发放套餐额度，不产生用户支付订单。</p>
      <ErrorMessage error={bind.error} />
      {bind.isSuccess && <p role="status">订阅已分配，编号 {String(bind.data.id)}。</p>}
      {!body && (
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            bind.reset()
            setBody({
              user_id: props.userID,
              plan_id: String(new FormData(event.currentTarget).get('bind-plan')),
              request_id: crypto.randomUUID(),
            })
          }}
        >
          <label className="field" htmlFor="bind-plan">
            <span>分配套餐</span>
            <select id="bind-plan" name="bind-plan" required>
              {props.plans.map((plan) => (
                <option value={String(plan.id)} key={String(plan.id)}>
                  {plan.name}
                  {plan.internal_only ? '（内部）' : ''}
                  {plan.enabled ? '' : '（已下架）'}
                </option>
              ))}
            </select>
          </label>
          <Button disabled={!props.plans.length} type="submit">
            核对分配
          </Button>
        </form>
      )}
      {body && (
        <div className="form-panel">
          <p className="full-width">
            确认向用户 {props.userID} 分配“
            {props.plans.find((plan) => String(plan.id) === String(body.plan_id))?.name}”？
          </p>
          <Button disabled={bind.isPending} onClick={() => bind.mutate(body)}>
            {bind.isPending ? '分配中…' : bind.isError ? '重试同一次分配' : '确认分配'}
          </Button>
          {!bind.isError && (
            <Button variant="quiet" disabled={bind.isPending} onClick={() => setBody(null)}>
              取消
            </Button>
          )}
        </div>
      )}
    </section>
  )
}
