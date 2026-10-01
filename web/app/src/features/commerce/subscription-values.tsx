import type { Schema } from '../../lib/types'
import { SubscriptionFuel } from './subscription-fuel'
import { SubscriptionConversion } from './subscription-conversion'

export { subscriptionConversionsOptions } from './subscription-conversion'

export function SubscriptionValues(props: {
  plans: Schema['Plan'][]
  subscriptions: Schema['Subscription'][]
  methods: Schema['PaymentMethod'][]
}) {
  const monthly = props.subscriptions.filter((subscription) => {
    const plan = props.plans.find((item) => String(item.id) === String(subscription.plan_id))
    return (
      subscription.state === 'active' &&
      new Date(subscription.expires_at).getTime() > Date.now() &&
      (!plan || plan.plan_type === 'monthly' || plan.duration_unit === 'month')
    )
  })
  return (
    <>
      <SubscriptionFuel {...props} subscriptions={monthly} />
      <SubscriptionConversion plans={props.plans} subscriptions={monthly} />
    </>
  )
}
