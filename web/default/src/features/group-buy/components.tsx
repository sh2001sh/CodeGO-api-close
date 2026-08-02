import { useEffect, useState } from 'react'
import { Clock3, Layers3, Sparkles, UsersRound } from 'lucide-react'
import collectiveBenefitHero from '@/assets/custom/collective-benefit-hero-compact.png'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { formatSubscriptionPlanTitle } from '@/features/subscriptions/lib'
import { cn } from '@/lib/utils'
import type { GroupBuyItem } from './types'

const BENEFITS = [
  { icon: Sparkles, text: '基础额度支付后立即到账' },
  { icon: UsersRound, text: '参与越多，加成档位越高' },
  { icon: Clock3, text: '满额或 48 小时统一结算' },
]

const TIER_NAMES: Record<number, string> = {
  1: '基础档',
  2: '进阶档',
  3: '优享档',
  5: '满享档',
}

export function CollectiveBenefitHero() {
  return (
    <section className='relative min-h-[310px] overflow-hidden rounded-2xl border border-white/10 bg-[#191512] text-white'>
      <img
        src={collectiveBenefitHero}
        alt=''
        aria-hidden='true'
        className='absolute inset-0 h-full w-full object-cover object-center opacity-60 sm:object-right sm:opacity-85'
      />
      <div className='absolute inset-0 bg-[linear-gradient(90deg,#191512_0%,rgba(25,21,18,0.96)_38%,rgba(25,21,18,0.2)_78%,rgba(25,21,18,0.05)_100%)]' />

      <div className='relative flex min-h-[310px] max-w-2xl flex-col justify-between p-6 sm:p-8 lg:p-10'>
        <div>
          <div className='mb-5 inline-flex items-center gap-2 rounded-full border border-amber-300/25 bg-amber-300/10 px-3 py-1.5 text-xs font-semibold text-amber-100'>
            <Layers3 className='h-3.5 w-3.5' />
            每一份参与，共同解锁更高额度
          </div>
          <h2 className='max-w-[12ch] text-3xl font-semibold tracking-[-0.03em] text-balance sm:text-4xl'>
            购买即参与，档位越高，额外额度越多
          </h2>
          <p className='mt-4 max-w-[54ch] text-sm leading-7 text-stone-300 sm:text-[15px]'>
            同一套餐的购买会自动汇入当前一期。基础额度立即生效，本期结束后按照最终参与档位为每位用户补发对应加成。
          </p>
        </div>

        <div className='mt-8 grid gap-3 border-t border-white/10 pt-5 sm:grid-cols-3'>
          {BENEFITS.map((benefit) => (
            <div
              key={benefit.text}
              className='flex items-center gap-2 text-xs text-stone-300'
            >
              <benefit.icon className='h-4 w-4 shrink-0 text-amber-300' />
              <span>{benefit.text}</span>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}

function formatRemaining(expiresAt: number, nowMs: number) {
  if (expiresAt <= 0) return '首位参与后开始 48 小时计时'
  const diff = Math.max(0, expiresAt * 1000 - nowMs)
  const totalMinutes = Math.ceil(diff / 60000)
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  if (hours <= 0) return `约 ${Math.max(1, minutes)} 分钟后结算`
  return `约 ${hours} 小时 ${minutes} 分钟后结算`
}

function bonusAtCount(item: GroupBuyItem, count: number) {
  if (count >= 5) return item.bonus_at_5
  if (count >= 3) return item.bonus_at_3
  if (count >= 2) return item.bonus_at_2
  return 0
}

function resolveUnlockedBonus(item: GroupBuyItem) {
  return bonusAtCount(item, item.current_count)
}

function resolveNextTier(item: GroupBuyItem) {
  if (item.current_count < 2) return { count: 2, bonus: item.bonus_at_2 }
  if (item.current_count < 3) return { count: 3, bonus: item.bonus_at_3 }
  if (item.current_count < 5) return { count: 5, bonus: item.bonus_at_5 }
  return null
}

export function GroupBuyCard(props: {
  item: GroupBuyItem
  onPurchase?: (item: GroupBuyItem) => void
}) {
  const [nowMs, setNowMs] = useState(() => Date.now())

  useEffect(() => {
    const timer = window.setInterval(() => setNowMs(Date.now()), 60000)
    return () => window.clearInterval(timer)
  }, [])

  const hasActivePeriod = props.item.id > 0
  const full = props.item.current_count >= props.item.target_count
  const closed = props.item.status !== 'pending'
  const unlockedBonus = resolveUnlockedBonus(props.item)
  const nextTier = resolveNextTier(props.item)
  const currentTotal = props.item.base_quota_usd + unlockedBonus
  const nextTierCopy = nextTier
    ? `再有 ${Math.max(1, nextTier.count - props.item.current_count)} 位参与，本期每人加成将提升至 +$${nextTier.bonus}`
    : `本期已解锁最高加成，每位参与者额外获得 +$${props.item.bonus_at_5}`

  return (
    <Card className='border-border bg-card h-full shadow-none transition-colors hover:border-primary/35'>
      <CardContent className='flex h-full flex-col gap-5 p-5'>
        <div className='flex items-start justify-between gap-4'>
          <div className='min-w-0'>
            <div className='mb-2 flex flex-wrap items-center gap-2'>
              <span className='border-primary/20 bg-primary/10 text-primary rounded-full border px-2.5 py-1 text-xs font-semibold'>
                {closed
                  ? '本期已结算'
                  : hasActivePeriod
                    ? '本期进行中'
                    : '等待开启'}
              </span>
              <span className='text-muted-foreground text-xs'>
                {props.item.current_count} 位已参与
              </span>
            </div>
            <h3 className='text-foreground truncate text-lg font-semibold'>
              {formatSubscriptionPlanTitle(props.item.plan_name)}
            </h3>
          </div>
          <div className='shrink-0 text-right'>
            <div className='text-foreground text-xl font-semibold tabular-nums'>
              ¥{props.item.plan_price}
            </div>
            <div className='text-muted-foreground mt-1 text-xs'>套餐价格</div>
          </div>
        </div>

        <div className='border-border/70 bg-muted/25 grid grid-cols-[1fr_auto_1fr_auto_1fr] items-center gap-2 rounded-xl border px-3 py-4 text-center'>
          <QuotaValue label='基础额度' value={`$${props.item.base_quota_usd}`} />
          <span className='text-muted-foreground text-lg'>+</span>
          <QuotaValue label='当前加成' value={`$${unlockedBonus}`} accent />
          <span className='text-muted-foreground text-lg'>=</span>
          <QuotaValue label='预计总额' value={`$${currentTotal}`} strong />
        </div>

        <div className='space-y-3'>
          <div className='flex items-center justify-between gap-3'>
            <span className='text-foreground text-sm font-semibold'>
              本期参与档位
            </span>
            <span className='text-muted-foreground flex items-center gap-1 text-xs tabular-nums'>
              <Clock3 className='h-3.5 w-3.5' />
              {hasActivePeriod
                ? formatRemaining(props.item.expires_at, nowMs)
                : '购买后开启本期'}
            </span>
          </div>
          <TierRail item={props.item} />
          <p className='text-muted-foreground text-sm leading-6'>
            {nextTierCopy}
          </p>
        </div>

        <Button
          className='mt-auto w-full'
          disabled={props.item.joined || full || closed}
          onClick={() => props.onPurchase?.(props.item)}
        >
          {props.item.joined
            ? '已参与本期'
            : full
              ? '本期已达满额档'
              : closed
                ? '本期已结算'
                : hasActivePeriod
                  ? '购买并参与本期'
                  : '购买并开启本期'}
        </Button>
      </CardContent>
    </Card>
  )
}

function TierRail({ item }: { item: GroupBuyItem }) {
  return (
    <div className='relative grid grid-cols-4 gap-1 pt-1'>
      <div className='bg-border absolute top-3.5 right-[12.5%] left-[12.5%] h-px' />
      {[1, 2, 3, 5].map((count) => {
        const active = item.current_count >= count
        const bonus = bonusAtCount(item, count)
        return (
          <div key={count} className='relative z-10 text-center'>
            <div
              className={cn(
                'mx-auto flex h-6 w-6 items-center justify-center rounded-full border text-[10px] font-semibold tabular-nums',
                active
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'border-border bg-card text-muted-foreground'
              )}
            >
              {count}
            </div>
            <div
              className={cn(
                'mt-2 text-xs font-semibold',
                active ? 'text-foreground' : 'text-muted-foreground'
              )}
            >
              {TIER_NAMES[count]}
            </div>
            <div className='text-muted-foreground mt-0.5 text-[11px] tabular-nums'>
              {bonus > 0 ? `+$${bonus}` : '基础额度'}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function QuotaValue(props: {
  label: string
  value: string
  accent?: boolean
  strong?: boolean
}) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground text-[11px]'>{props.label}</div>
      <div
        className={cn(
          'mt-1 truncate text-sm font-semibold tabular-nums sm:text-base',
          props.accent && 'text-primary',
          props.strong && 'text-foreground'
        )}
      >
        {props.value}
      </div>
    </div>
  )
}
