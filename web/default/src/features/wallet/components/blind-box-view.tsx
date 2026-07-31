import { AlertCircle, ChevronDown, Gift, Loader2, PackageOpen, Sparkles } from 'lucide-react'
import { motion, useReducedMotion, type Variants } from 'motion/react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type {
  BlindBoxProp,
  BlindBoxRewardStatistics,
  BlindBoxSelfData,
  BlindBoxTier,
  PaymentMethod,
} from '../types'
import { PaymentMethodSelector, PityStatusCard } from './blind-box-view-parts'

const STACK: Variants = {
  hidden: {},
  visible: { transition: { staggerChildren: 0.07, delayChildren: 0.05 } },
}

function getRewardStatistics(
  statistics: BlindBoxRewardStatistics[] | undefined,
  rewardType: string
) {
  return statistics?.find((reward) => reward.reward_type === rewardType)
}

function formatRewardSummary(
  reward: BlindBoxRewardStatistics | undefined,
  includeAmount: boolean
) {
  if (!reward || reward.opened_count === 0) return '0 次'
  if (!includeAmount || reward.reward_usd <= 0) {
    return `${reward.opened_count} 次`
  }
  return `${reward.opened_count} 次 · $${reward.reward_usd.toFixed(2)}`
}

interface BlindBoxCardViewProps {
  data: BlindBoxSelfData | null
  loading: boolean
  selectedQuantity: number
  selectedPaymentMethod: PaymentMethod | null
  amountDue: number
  paying: boolean
  openingCount: number | null
  availableBoxes: number
  effectivePityThreshold: number
  pityProgress: number
  remainingPity: number
  showPrizeNotice: boolean
  onQuantityChange: (value: number) => void
  onPaymentMethodChange: (method: PaymentMethod) => void
  onPay: () => void
  onManualOpen: (count: number) => void
  onUseProp: (prop: BlindBoxProp) => void
  onOpenProps: () => void
  onTogglePrizeNotice: () => void
  onClosePrizeNotice: () => void
}

function resolveTierRewardType(tier: BlindBoxTier) {
  if (tier.reward_type) return tier.reward_type
  if (tier.min_usd === 0 && tier.max_usd === 0) return 'prop'
  if (
    tier.wallet_type === 'claude' ||
    tier.name.toLowerCase().includes('claude')
  ) {
    return 'claude_quota'
  }
  return 'quota'
}

function formatBlindBoxTierLabel(tier: BlindBoxTier) {
  const rewardType = resolveTierRewardType(tier)
  if (rewardType === 'prop' || rewardType === 'subscription') {
    return tier.name
  }

  const amountText =
    tier.min_usd === tier.max_usd
      ? `$${tier.min_usd}`
      : `$${tier.min_usd} - $${tier.max_usd}`

  if (rewardType === 'claude_quota') {
    return `${amountText} Claude 额度`
  }
  return `${amountText} 普通额度`
}

function groupBlindBoxTiers(tiers: BlindBoxTier[]) {
  return {
    quota: tiers.filter((tier) => resolveTierRewardType(tier) === 'quota'),
    claude: tiers.filter(
      (tier) => resolveTierRewardType(tier) === 'claude_quota'
    ),
    props: tiers.filter((tier) => resolveTierRewardType(tier) === 'prop'),
  }
}

export function BlindBoxCardView(props: BlindBoxCardViewProps) {
  const shouldReduceMotion = Boolean(useReducedMotion())
  const firstPurchaseStartUSD = props.data?.first_purchase_guarantee_usd ?? 0
  const firstPurchaseEligible =
    props.data?.first_purchase_guarantee_eligible ?? false
  const groupedTiers = groupBlindBoxTiers(props.data?.tiers || [])
  const statistics = props.data?.statistics
  const standardRewards = getRewardStatistics(statistics?.rewards, 'quota')
  const claudeRewards = getRewardStatistics(statistics?.rewards, 'claude_quota')
  const propRewards = getRewardStatistics(statistics?.rewards, 'prop')
  const subscriptionRewards = getRewardStatistics(
    statistics?.rewards,
    'subscription'
  )

  return (
    <motion.div
      className='space-y-5'
      variants={STACK}
      initial={shouldReduceMotion ? false : 'hidden'}
      animate='visible'
    >
      {props.availableBoxes > 0 ? (
        <div className='flex items-start gap-3 rounded-xl border border-primary/20 bg-primary/6 p-4'>
          <div className='flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary'>
            <AlertCircle className='size-5' />
          </div>
          <div className='min-w-0 flex-1'>
            <div className='text-foreground text-sm font-semibold'>
              有 {props.availableBoxes} 次待抽取
            </div>
            <div className='text-muted-foreground mt-1 text-sm leading-6'>
              来自之前的订单，立即抽取不会重复扣费
            </div>
            <Button
              type='button'
              size='sm'
              className='mt-3'
              onClick={() => props.onManualOpen(props.availableBoxes)}
              disabled={props.openingCount !== null}
            >
              {props.openingCount === props.availableBoxes
                ? '处理中...'
                : `立即抽取 ${props.availableBoxes} 次`}
            </Button>
          </div>
        </div>
      ) : null}

      <PityStatusCard
        firstPurchaseEligible={firstPurchaseEligible}
        firstPurchaseUsd={firstPurchaseStartUSD}
        pityProgress={props.pityProgress}
        pityThreshold={props.effectivePityThreshold}
        remainingPity={props.remainingPity}
      />

      <div className='app-subtle-panel p-4'>
        <div className='flex flex-wrap items-baseline justify-between gap-2'>
          <div className='text-foreground text-sm font-semibold'>
            我的开盒统计
          </div>
          <div className='text-foreground text-sm font-semibold tabular-nums'>
            累计 {statistics?.total_opened || 0} 个
            {statistics?.pity_wins ? ` · 保底 ${statistics.pity_wins} 次` : ''}
          </div>
        </div>
        <div className='mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-xs sm:grid-cols-4'>
          <RewardStatistic
            label='普通额度'
            value={formatRewardSummary(standardRewards, true)}
          />
          <RewardStatistic
            label='Claude 额度'
            value={formatRewardSummary(claudeRewards, true)}
          />
          <RewardStatistic
            label='道具'
            value={formatRewardSummary(propRewards, false)}
          />
          <RewardStatistic
            label='套餐'
            value={formatRewardSummary(subscriptionRewards, false)}
          />
        </div>
      </div>

      <div>
        <div className='flex items-center justify-between gap-3'>
          <div className='text-foreground text-base font-semibold'>
            选择数量
          </div>
          <div className='text-muted-foreground text-sm'>
            单价 ¥{props.data?.unit_price?.toFixed(1) || '0.0'}
          </div>
        </div>

        <div className='mt-3 flex flex-wrap gap-2'>
          {(props.data?.count_options || [1, 3, 5, 10]).map((value) => (
            <QuantityChip
              key={value}
              value={value}
              current={props.selectedQuantity}
              onSelect={props.onQuantityChange}
            />
          ))}
          <Input
            type='number'
            min={1}
            value={props.selectedQuantity}
            onChange={(event) => {
              const value = Number(event.target.value)
              props.onQuantityChange(
                Number.isFinite(value) && value > 0 ? value : 1
              )
            }}
            className='h-9 max-w-24'
            aria-label='自定义数量'
            disabled={!props.data?.enabled || props.loading}
          />
        </div>
      </div>

      <div>
        <div className='text-foreground text-base font-semibold'>支付方式</div>
        <PaymentMethodSelector
          methods={props.data?.pay_methods || []}
          current={props.selectedPaymentMethod}
          disabled={!props.data?.enabled || props.loading}
          onSelect={props.onPaymentMethodChange}
        />
      </div>

      <div className='border-border/50 from-background to-muted/30 overflow-hidden rounded-2xl border bg-gradient-to-br p-4 shadow-sm'>
        <div className='flex flex-wrap items-center justify-between gap-4'>
          <div>
            <div className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
              应付金额
            </div>
            <div className='text-foreground mt-1 text-2xl font-semibold tabular-nums'>
              ¥{props.amountDue.toFixed(2)}
            </div>
          </div>
          <div className='flex flex-wrap gap-2'>
            <Button type='button' variant='outline' onClick={props.onOpenProps}>
              <PackageOpen className='size-4' data-icon='inline-start' />
              我的道具{props.data?.props?.length ? ` (${props.data.props.length})` : ''}
            </Button>
            <Button
              type='button'
              variant='outline'
              onClick={props.onTogglePrizeNotice}
            >
              <Gift className='size-4' data-icon='inline-start' />
              查看奖池
            </Button>
            <Button
              onClick={props.onPay}
              disabled={
                !props.data?.enabled ||
                props.paying ||
                !props.selectedPaymentMethod
              }
              className='min-w-36'
            >
              {props.paying ? (
                <>
                  <Loader2 data-icon='inline-start' className='animate-spin' />
                  处理中
                </>
              ) : (
                <>
                  <Sparkles className='size-4' data-icon='inline-start' />
                  立即购买
                </>
              )}
            </Button>
          </div>
        </div>
      </div>

      {props.showPrizeNotice ? (
        <div className='overview-glass-card rounded-2xl p-4'>
          <div className='mb-3 flex items-center justify-between gap-3'>
            <div className='text-foreground text-sm font-semibold'>
              盲盒奖池
            </div>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={props.onClosePrizeNotice}
            >
              <ChevronDown className='size-4' />
              收起
            </Button>
          </div>
          <div className='space-y-4 text-sm'>
            <div className='rounded-xl border border-primary/20 bg-primary/6 p-3'>
              <div className='text-foreground text-sm font-semibold'>奖池怎么抽</div>
              <div className='text-muted-foreground mt-1 text-xs leading-5'>
                每次开盒都会从普通额度、Claude 额度、道具和隐藏款中抽取。首购奖励与连续未中高价值奖励的保底优先结算。
              </div>
            </div>

            <div className='rounded-xl border border-amber-500/25 bg-amber-500/8 p-3'>
              <div className='text-foreground text-sm font-semibold'>
                特殊隐藏款：1 小时 0 倍率卡
              </div>
              <div className='text-muted-foreground mt-1 text-xs leading-5'>
                当前概率 {((props.data?.zero_hour?.current_probability || 0) * 100).toFixed(3)}%，进度 {props.data?.zero_hour?.points || 0}/{props.data?.zero_hour?.point_cap || 0}，最高 {((props.data?.zero_hour?.max_probability || 0) * 100).toFixed(3)}%。
              </div>
              <div className='text-muted-foreground mt-1 text-xs leading-5'>
                每成功结算 $1 增加 1 点，每个实际支付盲盒增加 5 点；抽中后进度归零。在“我的道具”启用后，zero-hour 分组持续 1 小时；default 分组内非生图模型按 0 倍率计费，仅限本人且并发最多 5 个请求。
              </div>
            </div>

            <div>
              <div className='text-foreground text-sm font-semibold'>
                奖励到账说明
              </div>
              <div className='text-muted-foreground mt-2 space-y-1.5 text-xs leading-5'>
                <div>普通额度会直接进入钱包，永久有效。</div>
                <div>Claude 额度会直接进入 Claude 钱包，永久有效。</div>
                <div>道具会进入盲盒页，按规则自动生效或手动启用。</div>
              </div>
            </div>

            <div>
              <div className='text-foreground text-sm font-semibold'>
                常规奖池
              </div>
              <div className='mt-2 space-y-3'>
                <div>
                  <div className='text-foreground text-xs font-medium'>
                    普通额度
                  </div>
                  <div className='mt-1.5 space-y-2'>
                    {groupedTiers.quota.map((tier) => (
                      <div
                        key={tier.name}
                        className='flex items-center justify-between'
                      >
                        <span className='text-foreground'>
                          {formatBlindBoxTierLabel(tier)}
                        </span>
                        <span className='text-muted-foreground font-medium tabular-nums'>
                          {(tier.probability * 100).toFixed(1)}%
                        </span>
                      </div>
                    ))}
                  </div>
                </div>

                <div>
                  <div className='text-foreground text-xs font-medium'>
                    Claude 额度
                  </div>
                  <div className='mt-1.5 space-y-2'>
                    {groupedTiers.claude.map((tier) => (
                      <div
                        key={tier.name}
                        className='flex items-center justify-between'
                      >
                        <span className='text-foreground'>
                          {formatBlindBoxTierLabel(tier)}
                        </span>
                        <span className='text-muted-foreground font-medium tabular-nums'>
                          {(tier.probability * 100).toFixed(1)}%
                        </span>
                      </div>
                    ))}
                  </div>
                </div>

                <div>
                  <div className='text-foreground text-xs font-medium'>
                    道具
                  </div>
                  <div className='mt-1.5 space-y-2'>
                    {groupedTiers.props.map((tier) => (
                      <div
                        key={tier.name}
                        className='flex items-center justify-between'
                      >
                        <span className='text-foreground'>{tier.name}</span>
                        <span className='text-muted-foreground font-medium tabular-nums'>
                          {(tier.probability * 100).toFixed(1)}%
                        </span>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            </div>

            <div>
              <div className='text-foreground text-sm font-semibold'>
                隐藏款
              </div>
              <div className='mt-2 flex items-center justify-between'>
                <span className='text-foreground'>
                  {(props.data?.subscription_plan_title || 'Lite 月卡') +
                    '（隐藏款）'}
                </span>
                <span className='text-muted-foreground font-medium tabular-nums'>
                  {(
                    (props.data?.subscription_prize_probability || 0) * 100
                  ).toFixed(1)}
                  %
                </span>
              </div>
            </div>

            <div>
              <div className='text-foreground text-sm font-semibold'>
                道具使用规则
              </div>
              <div className='text-muted-foreground mt-2 space-y-1.5 text-xs leading-5'>
                <div>充值九折卡：下次充值自动抵扣一次，仅生效 1 次。</div>
                <div>套餐九折卡：下次购买套餐自动抵扣一次，仅生效 1 次。</div>
                <div>0.95 倍率卡：在盲盒页点击使用后生效，持续 24 小时。</div>
                <div>0.9 倍率卡：在盲盒页点击使用后生效，持续 24 小时。</div>
                <div>1 小时 0 倍率卡：在“我的道具”启用，使用 zero-hour 分组；到期后该分组自动隐藏。</div>
              </div>
            </div>

            <div>
              <div className='text-foreground text-sm font-semibold'>
                保底规则
              </div>
              <div className='text-muted-foreground mt-2 space-y-1.5 text-xs leading-5'>
                <div>
                  连续 {props.data?.pity_threshold || 0}{' '}
                  次未获得高价值奖励时，当次触发保底。
                </div>
                <div>
                  保底奖励按 ${(props.data?.pity_guarantee_usd || 0).toFixed(0)}{' '}
                  美元档位及以上发放。
                </div>
              </div>
            </div>

            <div>
              <div className='text-foreground text-sm font-semibold'>
                首抽奖励
              </div>
              <div className='text-muted-foreground mt-2 text-xs leading-5'>
                首购保底20刀普通额度。首次购买盲盒后，首抽普通额度最低保底 $
                {firstPurchaseStartUSD.toFixed(0)}。
              </div>
            </div>
          </div>
        </div>
      ) : null}
    </motion.div>
  )
}

function RewardStatistic(props: { label: string; value: string }) {
  return (
    <div className='min-w-0'>
      <div className='text-muted-foreground'>{props.label}</div>
      <div className='text-foreground mt-0.5 truncate font-medium tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}

function QuantityChip(props: {
  value: number
  current: number
  onSelect: (value: number) => void
}) {
  const active = props.value === props.current

  return (
    <button
      type='button'
      onClick={() => props.onSelect(props.value)}
      className={cn(
        'rounded-full border px-3.5 py-1.5 text-sm font-medium transition-colors',
        active
          ? 'border-primary bg-primary text-primary-foreground'
          : 'border-border bg-background/80 text-foreground hover:border-foreground'
      )}
    >
      x{props.value}
    </button>
  )
}
