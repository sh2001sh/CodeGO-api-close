import { Link } from '@tanstack/react-router'
import { Ticket } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Card } from '@/components/ui/card'
import type { LuckyNumberSubscription, LuckyDrawView, LuckyRewardView } from '../types'
import { SubscriptionLuckySummary } from './subscription-lucky-summary'

export function LuckySubscriptionList(props: {
  subscriptions: LuckyNumberSubscription[]
  draw?: LuckyDrawView
  rewards: LuckyRewardView[]
}) {
  const { t } = useTranslation()
  return (
    <section className='space-y-3'>
      <div className='flex flex-wrap items-end justify-between gap-2'>
        <div>
          <h2 className='text-foreground text-base font-semibold tracking-tight'>
            {t('My monthly card numbers')}
          </h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Numbers remain yours after expiry, while only active eligible cards enter future draws.')}
          </p>
        </div>
        <span className='text-muted-foreground text-xs tabular-nums'>
          {t('{{count}} eligible cards', { count: props.subscriptions.length })}
        </span>
      </div>

      {props.subscriptions.length === 0 ? (
        <Card className='shadow-none'>
          <Empty className='border-0'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Ticket aria-hidden='true' />
              </EmptyMedia>
              <EmptyTitle>{t('No eligible monthly card')}</EmptyTitle>
              <EmptyDescription>
                {t('Buy an eligible monthly plan to receive a permanent number and join the next draw automatically.')}
              </EmptyDescription>
            </EmptyHeader>
            <Button render={<Link to='/packages' />}>{t('Browse plans')}</Button>
          </Empty>
        </Card>
      ) : (
        <div className='divide-border app-page-shell divide-y overflow-hidden'>
          {props.subscriptions.map((item) => (
            <div
              key={item.subscription.id}
              className={cn('p-4 transition-colors sm:p-5', 'hover:bg-muted/25')}
            >
              <SubscriptionLuckySummary
                record={{ subscription: item.subscription }}
                plan={item.plan}
                draw={props.draw}
                rewards={props.rewards}
                showLink={false}
              />
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
