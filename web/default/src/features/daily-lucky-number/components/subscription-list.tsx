import { Link } from '@tanstack/react-router'
import { Ticket } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import type { LuckyDrawView, LuckyNumberRules, LuckyNumberSubscription, LuckyRewardView } from '../types'
import { normalizeLuckyNumberRules } from '../lib'
import { SubscriptionLuckySummary } from './subscription-lucky-summary'

export function LuckySubscriptionList(props: {
  subscriptions: LuckyNumberSubscription[]
  draw?: LuckyDrawView
  rewards: LuckyRewardView[]
  rules?: Partial<LuckyNumberRules> | null
}) {
  const rules = normalizeLuckyNumberRules(props.rules)

  return (
    <section className='space-y-3'>
      <div className='flex flex-wrap items-end justify-between gap-2'>
        <div>
          <h2 className='text-foreground text-base font-semibold tracking-tight'>
            我的月卡号码
          </h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            号码永久保留，只有有效期内的月卡会参与后续开奖。
          </p>
        </div>
        <span className='text-muted-foreground text-xs tabular-nums'>
          {props.subscriptions.length} 个有效号码
        </span>
      </div>

      {props.subscriptions.length === 0 ? (
        <div className='app-page-shell'>
          <Empty className='border-0'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <Ticket aria-hidden='true' />
              </EmptyMedia>
            <EmptyTitle>暂时没有可参与的月卡</EmptyTitle>
              <EmptyDescription>
                购买符合条件的月卡即可获得专属号码，并自动参与下一期开奖。
              </EmptyDescription>
            </EmptyHeader>
            <Button render={<Link to='/packages' />}>查看套餐</Button>
          </Empty>
        </div>
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
                tierRules={rules}
                showLink={false}
              />
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
