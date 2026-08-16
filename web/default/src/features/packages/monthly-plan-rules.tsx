import {
  ArrowRightLeft,
  CircleDollarSign,
  Gift,
  RefreshCw,
  Sparkles,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

const multiplierDurations = [
  ['Lite', '15 分钟'],
  ['Standard', '30 分钟'],
  ['Pro', '45 分钟'],
  ['Ultra', '60 分钟'],
] as const

const comparisonRows = [
  {
    label: '有效期',
    monthly: '购买后 1 个月内有效',
    wallet: '永久有效，不自动过期',
  },
  {
    label: '扣费范围',
    monthly: '仅限管理员开启套餐扣费的官方分组，按分组套餐倍率扣费',
    wallet: '按余额倍率扣费，支持范围更广，可用于获准的第三方渠道',
  },
  {
    label: '0.1 倍率卡',
    monthly: '实际扣月卡额度时，在分组套餐倍率基础上再乘 0.1',
    wallet: '从通用余额扣费时不应用套餐卡，按原余额倍率计费',
  },
  {
    label: '额度刷新',
    monthly: '符合条件时可用邀请奖励清空已用额度，但不延长有效期',
    wallet: '不支持刷新',
  },
  {
    label: '每日幸运号',
    monthly: '每张有效月卡自动参与每日开奖',
    wallet: '不参与月卡幸运号开奖',
  },
] as const

export function MonthlyPlanRules() {
  const { t } = useTranslation()

  return (
    <section
      aria-labelledby='monthly-plan-rules-title'
      className='border-border overflow-hidden rounded-lg border'
    >
      <div className='bg-primary/[0.055] flex items-start gap-3 px-4 py-4 sm:px-5'>
        <CircleDollarSign
          className='text-primary mt-0.5 size-5 shrink-0'
          aria-hidden='true'
        />
        <div className='min-w-0'>
          <h3
            id='monthly-plan-rules-title'
            className='text-foreground text-base font-semibold'
          >
            {t('购买前请先了解月卡规则')}
          </h3>
          <p className='text-muted-foreground mt-1 max-w-3xl text-sm leading-6'>
            {t(
              '月卡与通用余额是两种独立资金：月卡有专属权益和使用范围，通用余额永久有效且适用范围更广。'
            )}
          </p>
        </div>
      </div>

      <div className='divide-border divide-y'>
        <div className='grid gap-5 px-4 py-4 sm:px-5 lg:grid-cols-[minmax(0,1fr)_minmax(300px,0.8fr)]'>
          <div>
            <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
              <Gift className='text-primary size-4' aria-hidden='true' />
              {t('购买月卡赠送 0.1 倍率卡')}
            </div>
            <p className='text-muted-foreground mt-2 text-sm leading-6'>
              {t(
                '倍率卡可随时启用、暂停和继续，只计算实际启用时间。多次获得的剩余时长会合并。'
              )}
            </p>
            <p className='text-foreground mt-2 text-sm leading-6 font-medium'>
              {t(
                '无需切换 API Key 分组。系统按资金源顺序结算：实际扣月卡额度时应用 0.1，使用或回退到通用余额时按原倍率扣费。'
              )}
            </p>
          </div>

          <dl className='grid grid-cols-2 gap-x-4 gap-y-2 text-sm'>
            {multiplierDurations.map(([tier, duration]) => (
              <div
                key={tier}
                className='border-border flex items-center justify-between gap-3 border-b py-1.5 last:border-b-0'
              >
                <dt className='text-muted-foreground'>{tier}</dt>
                <dd className='text-foreground font-semibold tabular-nums'>
                  {t(duration)}
                </dd>
              </div>
            ))}
          </dl>
        </div>

        <div className='px-4 py-4 sm:px-5'>
          <h4 className='text-foreground text-sm font-semibold'>
            {t('月卡与通用余额的区别')}
          </h4>
          <div className='border-border mt-3 overflow-x-auto rounded-md border'>
            <div
              role='table'
              aria-label={t('月卡与通用余额对照')}
              className='min-w-[680px] text-sm'
            >
              <div
                role='row'
                className='bg-muted/45 text-muted-foreground grid grid-cols-[130px_minmax(0,1fr)_minmax(0,1fr)] font-medium'
              >
                <div role='columnheader' className='px-3 py-2.5'>
                  {t('对比项目')}
                </div>
                <div role='columnheader' className='px-3 py-2.5'>
                  {t('月卡额度')}
                </div>
                <div role='columnheader' className='px-3 py-2.5'>
                  {t('通用余额')}
                </div>
              </div>
              <div className='divide-border divide-y'>
                {comparisonRows.map((row) => (
                  <div
                    key={row.label}
                    role='row'
                    className='grid grid-cols-[130px_minmax(0,1fr)_minmax(0,1fr)]'
                  >
                    <div
                      role='rowheader'
                      className='text-foreground px-3 py-3 font-medium'
                    >
                      {t(row.label)}
                    </div>
                    <div
                      role='cell'
                      className='text-muted-foreground px-3 py-3 leading-5'
                    >
                      {t(row.monthly)}
                    </div>
                    <div
                      role='cell'
                      className='text-muted-foreground px-3 py-3 leading-5'
                    >
                      {t(row.wallet)}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>

        <div className='grid gap-5 px-4 py-4 sm:px-5 lg:grid-cols-2'>
          <div>
            <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
              <Sparkles className='text-warning size-4' aria-hidden='true' />
              {t('每张月卡都有幸运号')}
            </div>
            <p className='text-muted-foreground mt-2 text-sm leading-6'>
              {t(
                '月卡生效后会获得一个四位幸运号，无需签到或报名，在月卡有效期内自动参加每日开奖。'
              )}
            </p>
          </div>
          <div>
            <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
              <RefreshCw className='text-primary size-4' aria-hidden='true' />
              {t('刷新与转余额只能二选一')}
            </div>
            <p className='text-muted-foreground mt-2 text-sm leading-6'>
              {t(
                '月卡折现为通用余额后，该月卡不能再刷新；月卡使用过额度刷新后，也不能再折现为通用余额。两项操作均不可撤销。'
              )}
            </p>
          </div>
        </div>

        <div className='bg-warning/[0.07] flex items-start gap-3 px-4 py-3.5 sm:px-5'>
          <ArrowRightLeft
            className='text-warning mt-0.5 size-4 shrink-0'
            aria-hidden='true'
          />
          <p className='text-foreground text-sm leading-6'>
            {t(
              '折现时可选择不超过未使用比例的整数百分比，到账通用余额 = 月卡档位价格 × 选择比例。部分折现后月卡继续生效，但从首次折现起永久失去刷新资格。'
            )}
          </p>
          <p className='text-muted-foreground text-sm leading-6'>
            {t(
              '使用邀请奖励刷新额度后，刷新得到的额度仅用于模型调用，不能抵扣该月卡续费或升级价格；续费按套餐原价计算，升级不再抵扣旧卡剩余额度价值。'
            )}
          </p>
        </div>
      </div>
    </section>
  )
}
