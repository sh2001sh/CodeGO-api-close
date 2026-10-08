import type { Schema } from '../../lib/types'
import {
  frozenSubscriptionPlan,
  subscriptionPolicy,
  validSubscription,
} from '../../lib/subscription-policy'
import { SubscriptionFuel } from './subscription-fuel'
import { SubscriptionConversion } from './subscription-conversion'
import { WholeWalletConversion } from './whole-wallet-conversion'

export { subscriptionConversionsOptions } from './subscription-conversion'

export function SubscriptionValues(props: {
  plans: Schema['Plan'][]
  subscriptions: Schema['Subscription'][]
  methods: Schema['PaymentMethod'][]
}) {
  const monthly = props.subscriptions.filter((subscription) => {
    const plan = frozenSubscriptionPlan(subscription, props.plans)
    return (
      validSubscription(subscription) &&
      subscriptionPolicy(subscription) === 'legacy' &&
      (!plan || plan.plan_type === 'monthly' || plan.duration_unit === 'month')
    )
  })
  return (
    <>
      <SubscriptionFuel {...props} subscriptions={monthly} />
      <SubscriptionConversion plans={props.plans} subscriptions={monthly} />
      <WholeWalletConversion plans={props.plans} subscriptions={props.subscriptions} />
    </>
  )
}
