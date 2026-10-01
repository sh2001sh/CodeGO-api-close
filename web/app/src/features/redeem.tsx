import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { api, unwrap } from '../lib/api'
import { credits } from '../lib/format'
import { Button, ErrorMessage, Field } from '../components/ui'

export function RedeemCredits() {
  const client = useQueryClient()
  const redeem = useMutation({
    mutationFn: (key: string) =>
      api.POST('/api/commerce/redemptions/redeem', { body: { key } }).then(unwrap),
    onSuccess: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: ['wallet'] }),
        client.invalidateQueries({ queryKey: ['subscriptions'] }),
        client.invalidateQueries({ queryKey: ['boxes'] }),
        client.invalidateQueries({ queryKey: ['orders'] }),
      ]),
  })
  return (
    <section className="section">
      <h2>兑换码</h2>
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          redeem.mutate(String(new FormData(event.currentTarget).get('redemption-key')).trim())
        }}
      >
        <Field name="redemption-key" label="兑换码" required maxLength={256} />
        <Button type="submit" disabled={redeem.isPending}>
          兑换
        </Button>
      </form>
      <ErrorMessage error={redeem.error} />
      {redeem.isSuccess && (
        <p role="status" className="notice">
          {redeem.data.redeem_type === 'subscription' ? (
            <>
              已兑换订阅：{redeem.data.plan_title || `套餐 ${redeem.data.plan_id}`}
              {redeem.data.user_subscription_id
                ? `（订阅 ${redeem.data.user_subscription_id}）`
                : ''}
            </>
          ) : redeem.data.redeem_type === 'blind_box' ? (
            <>
              已兑换 {redeem.data.blind_box_quantity} 个盲盒。 <Link to="/blind-box">查看盲盒</Link>
            </>
          ) : (
            <>已兑换 {credits(redeem.data.credits)}</>
          )}
        </p>
      )}
    </section>
  )
}
