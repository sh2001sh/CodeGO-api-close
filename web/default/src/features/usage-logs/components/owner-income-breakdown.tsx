import { getIntlLocale } from '@/i18n/format'
import { CircleHelp, Layers3, Wallet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuotaWithCurrency } from '@/lib/currency'
import type { MarketplaceOwnerIncomeBreakdown } from '@/features/marketplace/types'

export function OwnerBillingSource(props: {
  breakdown?: MarketplaceOwnerIncomeBreakdown | null
}) {
  const { t } = useTranslation()
  const detail = props.breakdown
  const wallet = detail?.billing_source === 'wallet'
  const subscription =
    detail?.billing_source === 'subscription' ||
    detail?.billing_source === 'credit_pack'
  const Icon = wallet ? Wallet : subscription ? Layers3 : CircleHelp
  const label = wallet
    ? t('钱包')
    : subscription
      ? detail?.subscription_plan_title || t('套餐')
      : t('Unknown')

  return (
    <span className='inline-flex max-w-full min-w-0 items-center gap-1 text-xs'>
      <Icon className='size-3 shrink-0' aria-hidden />
      <span className='truncate' title={label}>
        {label}
      </span>
      {subscription && detail?.subscription_id != null && (
        <span className='shrink-0 font-mono'>#{detail.subscription_id}</span>
      )}
    </span>
  )
}

export function OwnerIncomeBreakdown(props: {
  breakdown?: MarketplaceOwnerIncomeBreakdown | null
}) {
  const { t } = useTranslation()
  const detail = props.breakdown
  const unknown = t('Unknown')
  const amount = (value: number | null | undefined) =>
    value == null || !Number.isFinite(value)
      ? unknown
      : formatQuotaWithCurrency(value, {
          digitsLarge: 6,
          digitsSmall: 6,
          abbreviate: false,
        })
  const rate = (value: number | null | undefined) =>
    value == null || !Number.isFinite(value)
      ? unknown
      : new Intl.NumberFormat(getIntlLocale(), {
          style: 'percent',
          maximumFractionDigits: 4,
        }).format(value)
  const subscription =
    detail?.billing_source === 'subscription' ||
    detail?.billing_source === 'credit_pack'
  const baseRate =
    detail?.base_commission_rate_bps == null
      ? null
      : detail.base_commission_rate_bps / 10_000
  const surchargeRate = detail?.package_surcharge_rate
  const totalRate =
    baseRate != null && surchargeRate != null ? baseRate + surchargeRate : null
  const rows: [string, React.ReactNode][] = [
    [t('扣费来源'), <OwnerBillingSource key='source' breakdown={detail} />],
    ...(subscription
      ? ([
          [t('套餐名称'), detail?.subscription_plan_title || unknown],
          [t('套餐实例 ID'), detail?.subscription_id ?? unknown],
          [t('套餐计划 ID'), detail?.subscription_plan_id ?? unknown],
        ] as [string, React.ReactNode][])
      : []),
    [t('结算毛额'), amount(detail?.settlement_gross_amount)],
    [t('基础佣金'), amount(detail?.base_commission_amount)],
    [t('基础佣金率'), rate(baseRate)],
    ...(subscription
      ? ([
          [t('套餐增额率'), rate(detail?.package_bonus_rate)],
          [
            t('渠道分担比例'),
            rate(
              detail?.channel_share_bps == null
                ? null
                : detail.channel_share_bps / 10_000
            ),
          ],
          [t('增额分担费率'), rate(detail?.package_surcharge_rate)],
        ] as [string, React.ReactNode][])
      : []),
    [t('增额分担费'), amount(detail?.package_surcharge_amount)],
    [t('综合手续费率'), rate(totalRate)],
    [t('邀请返利'), amount(detail?.referral_rebate_amount)],
    [t('净收益'), amount(detail?.owner_net_amount)],
    [t('结算版本'), detail?.algorithm_version || unknown],
  ]

  return (
    <div data-owner-income-breakdown>
      {detail?.snapshot_status === 'historical_partial' && (
        <div className='text-muted-foreground mb-3 text-xs'>
          {t('历史快照')}
        </div>
      )}
      <dl className='space-y-2'>
        {rows.map(([label, value]) => (
          <div
            key={label}
            className='grid grid-cols-[7rem_minmax(0,1fr)] gap-3 text-xs'
          >
            <dt className='text-muted-foreground'>{label}</dt>
            <dd className='min-w-0 break-words tabular-nums'>{value}</dd>
          </div>
        ))}
      </dl>
      <div className='bg-muted/50 mt-4 space-y-2 rounded-lg p-3 text-xs'>
        <div>{t('净收益 = 毛额 − 基础佣金 − 增额分担 − 邀请返利')}</div>
        <div className='font-mono break-all tabular-nums'>
          {detail
            ? `${amount(detail.settlement_gross_amount)} − ${amount(detail.base_commission_amount)} − ${amount(detail.package_surcharge_amount)} − ${amount(detail.referral_rebate_amount)} = ${amount(detail.owner_net_amount)}`
            : unknown}
        </div>
      </div>
    </div>
  )
}
