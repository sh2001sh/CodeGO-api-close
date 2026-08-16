import { Link } from '@tanstack/react-router'
import { Gift, RefreshCw, Settings2, Wallet } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  getOptionValue,
  useSystemOptions,
} from '@/features/system-settings/hooks/use-system-options'
import type { BillingSettings } from '@/features/system-settings/types'
import { BalanceBlindBoxSettingsAdmin } from './balance-blind-box-settings-admin'

const DEFAULT_BLIND_BOX_SETTINGS: Pick<
  BillingSettings,
  | 'blind_box_setting.enabled'
  | 'blind_box_setting.unit_price'
  | 'blind_box_setting.daily_limit'
  | 'blind_box_setting.monthly_limit'
  | 'blind_box_setting.daily_open_limit'
  | 'blind_box_setting.first_purchase_guarantee_usd'
  | 'blind_box_setting.pity_threshold'
  | 'blind_box_setting.pity_guarantee_usd'
  | 'blind_box_setting.low_reward_threshold_usd'
  | 'blind_box_setting.subscription_prize_probability'
  | 'blind_box_setting.subscription_plan_title'
  | 'blind_box_setting.count_options'
  | 'blind_box_setting.tiers'
> = {
  'blind_box_setting.enabled': false,
  'blind_box_setting.unit_price': 2.5,
  'blind_box_setting.daily_limit': 50,
  'blind_box_setting.monthly_limit': 500,
  'blind_box_setting.daily_open_limit': 5000,
  'blind_box_setting.first_purchase_guarantee_usd': 10,
  'blind_box_setting.pity_threshold': 5,
  'blind_box_setting.pity_guarantee_usd': 10,
  'blind_box_setting.low_reward_threshold_usd': 5,
  'blind_box_setting.subscription_prize_probability': 0.003,
  'blind_box_setting.subscription_plan_title': 'Standard月卡',
  'blind_box_setting.count_options': [1, 5, 10, 20, 50],
  'blind_box_setting.tiers': [
    { name: 'starter', min_usd: 1, max_usd: 3, probability: 0.18 },
    { name: 'steady', min_usd: 4, max_usd: 7, probability: 0.3 },
    { name: 'core', min_usd: 8, max_usd: 12, probability: 0.31 },
    { name: 'boost', min_usd: 13, max_usd: 20, probability: 0.15 },
    { name: 'lucky', min_usd: 21, max_usd: 50, probability: 0.057 },
  ],
}

export function BlindBoxOperationsPanel() {
  const optionsQuery = useSystemOptions()
  const settings = getOptionValue(
    optionsQuery.data?.data,
    DEFAULT_BLIND_BOX_SETTINGS
  )

  return (
    <div className='space-y-4'>
      <div className='rounded-2xl border bg-slate-50/70 p-4'>
        <div className='flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between'>
          <div className='space-y-2'>
            <div className='flex items-center gap-2 text-sm font-semibold text-slate-950'>
              <Gift className='h-4 w-4 text-amber-600' />
              盲盒运营面板
            </div>
            <p className='text-muted-foreground text-sm leading-6'>
              在这里查看盲盒规则、价格和奖池概率，并跳转到配置页进行调整。
            </p>
          </div>

          <div className='flex flex-wrap gap-2'>
            <Button
              variant='outline'
              size='sm'
              onClick={() => void optionsQuery.refetch()}
              disabled={optionsQuery.isFetching}
            >
              <RefreshCw
                className={cn(
                  'mr-1 h-4 w-4',
                  optionsQuery.isFetching && 'animate-spin'
                )}
              />
              刷新
            </Button>
            <Button
              variant='outline'
              size='sm'
              render={
                <Link
                  to='/system-settings/billing/$section'
                  params={{ section: 'blind-box' }}
                />
              }
            >
              <Settings2 className='mr-1 h-4 w-4' />
              打开配置
            </Button>
            <Button
              size='sm'
              onClick={() => window.location.assign('/blind-box')}
            >
              <Wallet className='mr-1 h-4 w-4' />
              打开盲盒页
            </Button>
          </div>
        </div>
      </div>

      <BalanceBlindBoxSettingsAdmin />

      <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
        <MetricCard
          label='活动状态'
          value={settings['blind_box_setting.enabled'] ? '已启用' : '未启用'}
        />
        <MetricCard
          label='单盒售价'
          value={`${settings['blind_box_setting.unit_price'].toFixed(2)} USD`}
        />
        <MetricCard
          label='月卡奖励概率'
          value={`${(
            settings['blind_box_setting.subscription_prize_probability'] * 100
          ).toFixed(2)}%`}
        />
        <MetricCard
          label='月卡奖励标题'
          value={settings['blind_box_setting.subscription_plan_title']}
        />
        <MetricCard
          label='首抽奖池起点'
          value={`${settings['blind_box_setting.first_purchase_guarantee_usd'].toFixed(2)} USD`}
        />
        <MetricCard
          label='每日购买上限'
          value={String(settings['blind_box_setting.daily_limit'])}
        />
        <MetricCard
          label='每月购买上限'
          value={String(settings['blind_box_setting.monthly_limit'])}
        />
        <MetricCard
          label='保底触发次数'
          value={String(settings['blind_box_setting.pity_threshold'])}
        />
      </div>

      <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]'>
        <div className='rounded-2xl border p-4'>
          <div className='text-sm font-semibold'>常规奖池档位</div>
          <div className='mt-4 space-y-3'>
            {settings['blind_box_setting.tiers'].map((tier) => (
              <div
                key={tier.name}
                className='flex items-center justify-between gap-3 rounded-xl border px-3 py-2'
              >
                <div>
                  <div className='font-medium'>{tier.name}</div>
                  <div className='text-muted-foreground text-xs'>
                    {tier.min_usd}-{tier.max_usd} USD
                  </div>
                </div>
                <div className='text-sm font-semibold'>
                  {(tier.probability * 100).toFixed(2)}%
                </div>
              </div>
            ))}
          </div>
        </div>

        <div className='space-y-4'>
          <div className='rounded-2xl border p-4 text-sm'>
            <div className='font-semibold'>首抽专属奖池规则</div>
            <div className='text-muted-foreground mt-2 leading-6'>
              首次抽取会优先使用首抽奖池；若未命中月卡，则继续按专属奖池规则发放，非月卡奖励从首抽起始金额开始，并提高高档位概率。
            </div>
          </div>

          <div className='rounded-2xl border p-4 text-sm'>
            <div className='font-semibold'>其他参数</div>
            <div className='text-muted-foreground mt-2 leading-6'>
              购买数量选项：
              {settings['blind_box_setting.count_options'].join(', ')}
            </div>
            <div className='text-muted-foreground mt-3 leading-6'>
              保底最低奖励：
              {settings['blind_box_setting.pity_guarantee_usd'].toFixed(2)} USD
            </div>
            <div className='text-muted-foreground leading-6'>
              低档奖励判定线：
              {settings['blind_box_setting.low_reward_threshold_usd'].toFixed(
                2
              )}{' '}
              USD
            </div>
            <div className='text-muted-foreground leading-6'>
              抽中的统一额度会直接进入账户，永久有效。
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function MetricCard(props: { label: string; value: string }) {
  return (
    <div className='rounded-xl border bg-white p-3'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-1 text-sm font-semibold break-all'>{props.value}</div>
    </div>
  )
}
