import { useState } from 'react'
import { ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { SubscriptionPurchaseDialog } from '@/features/subscriptions/components/dialogs/subscription-purchase-dialog'
import {
  formatSubscriptionPlanPrice,
  formatSubscriptionQuotaAmount,
} from '@/features/subscriptions/lib'
import type { PlanRecord } from '@/features/subscriptions/types'
import type { TopupInfo } from '../types'
import { getEpayMethods } from './subscription-plans-card'

export function CreditPackPlans(props: {
  plans: PlanRecord[]
  loading?: boolean
  topupInfo: TopupInfo | null
  onRefresh: () => Promise<void>
}) {
  const { t } = useTranslation()
  const [selected, setSelected] = useState<PlanRecord | null>(null)
  const plans = props.plans
    .filter((record) => record.plan.plan_type === 'credit_pack')
    .sort(
      (a, b) => b.plan.sort_order - a.plan.sort_order || a.plan.id - b.plan.id
    )
  if (props.loading)
    return <Skeleton className='h-72 w-full' aria-label={t('Loading')} />
  if (plans.length === 0) return null

  return (
    <section
      aria-labelledby='credit-pack-title'
      className='commerce-credit-packs'
    >
      <header className='commerce-credit-header'>
        <h2 id='credit-pack-title' className='text-lg font-semibold'>
          {t('永久额度包')}
        </h2>
      </header>
      <div className='commerce-credit-list'>
        {plans.map((record) => {
          const plan = record.plan
          const bonus =
            plan.currency === 'CNY' && plan.price_amount > 0
              ? Math.max(0, plan.total_amount - plan.price_amount * 500_000)
              : undefined
          return (
            <article key={plan.id} className='commerce-credit-record'>
              <div>
                <div className='commerce-plan-identity'>
                  <h3>{plan.title}</h3>
                </div>
                <span className='commerce-credit-price'>
                  {formatSubscriptionPlanPrice(
                    plan.price_amount,
                    plan.currency
                  )}
                </span>
              </div>
              <dl className='commerce-credit-quota'>
                <div>
                  <dt>{t('Quota')}</dt>
                  <dd>{formatSubscriptionQuotaAmount(plan.total_amount)}</dd>
                </div>
                {bonus !== undefined && bonus > 0 ? (
                  <div>
                    <dt>{t('赠额')}</dt>
                    <dd>+{formatSubscriptionQuotaAmount(bonus)}</dd>
                  </div>
                ) : null}
              </dl>
              <Button
                className='commerce-credit-purchase'
                onClick={() => setSelected(record)}
                aria-label={`${t('购买')} ${plan.title}`}
              >
                {t('购买')}
                <ArrowUpRight className='size-4' aria-hidden />
              </Button>
            </article>
          )
        })}
      </div>
      <SubscriptionPurchaseDialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) {
            setSelected(null)
            void props.onRefresh()
          }
        }}
        plan={selected}
        enableStripe={!!props.topupInfo?.enable_stripe_topup}
        enableCreem={!!props.topupInfo?.enable_creem_topup}
        enableOnlineTopUp={!!props.topupInfo?.enable_online_topup}
        epayMethods={getEpayMethods(props.topupInfo?.pay_methods)}
        purchaseType='normal'
      />
    </section>
  )
}
