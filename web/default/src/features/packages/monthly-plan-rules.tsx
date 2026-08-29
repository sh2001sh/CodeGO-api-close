/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useTranslation } from 'react-i18next'

const multiplierDurations = [
  ['Lite', '15 分钟'],
  ['Standard', '30 分钟'],
  ['Pro', '45 分钟'],
  ['Ultra', '60 分钟'],
] as const

const multiplierRules = [
  ['扣月卡额度', '倍率 ×0.1'],
  ['扣通用余额', '原倍率'],
  ['剩余时长', '自动合并'],
] as const

const comparisonRows = [
  {
    label: '有效期',
    monthly: '购买后 1 个月内',
    wallet: '永久有效',
  },
  {
    label: '扣费范围',
    monthly: '套餐扣费分组 · 套餐倍率',
    wallet: '官方分组与获准第三方渠道 · 余额倍率',
  },
  {
    label: '0.1 倍率卡',
    monthly: '套餐倍率基础上 ×0.1',
    wallet: '不应用',
  },
  {
    label: '额度刷新',
    monthly: '邀请奖励可清空已用',
    wallet: '不支持',
  },
  {
    label: '每日幸运号',
    monthly: '自动参与开奖',
    wallet: '不参与',
  },
] as const

export function MonthlyPlanRules() {
  const { t } = useTranslation()

  return (
    <section
      aria-labelledby='monthly-plan-rules-title'
      className='codego-panel overflow-hidden'
    >
      <div className='flex items-center justify-between gap-3 border-b px-4 py-3 sm:px-5'>
        <div className='flex items-center gap-2.5'>
          <span aria-hidden className='bg-primary block h-3 w-[3px]' />
          <h3
            id='monthly-plan-rules-title'
            className='text-foreground text-[13px] font-semibold'
          >
            {t('月卡规则')}
          </h3>
        </div>
        <span className='codego-stat-label'>SPEC</span>
      </div>

      <div className='grid lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]'>
        <div className='border-b px-4 py-5 sm:px-5 lg:border-r lg:border-b-0'>
          <span className='codego-kicker'>MULTIPLIER CARD</span>
          <dl className='mt-4 grid grid-cols-2 gap-x-8'>
            {multiplierDurations.map(([tier, duration]) => (
              <div
                key={tier}
                className='border-border/70 flex items-baseline justify-between gap-3 border-t py-2.5 first:border-t-0 first:pt-0'
              >
                <dt className='codego-stat-label'>{tier}</dt>
                <dd className='text-foreground text-sm font-semibold tabular-nums'>
                  {t(duration)}
                </dd>
              </div>
            ))}
          </dl>
        </div>

        <div className='px-4 py-5 sm:px-5'>
          <span className='codego-kicker'>BILLING</span>
          <dl className='mt-4'>
            {multiplierRules.map(([label, value]) => (
              <div
                key={label}
                className='border-border/70 flex items-baseline justify-between gap-3 border-t py-2.5 first:border-t-0 first:pt-0'
              >
                <dt className='codego-stat-label'>{t(label)}</dt>
                <dd className='text-foreground text-sm font-semibold tabular-nums'>
                  {t(value)}
                </dd>
              </div>
            ))}
          </dl>
        </div>
      </div>

      <div className='border-t px-4 py-5 sm:px-5'>
        <span className='codego-kicker'>月卡额度 / 通用余额</span>
        <div className='mt-4 overflow-x-auto'>
          <div
            role='table'
            aria-label={t('月卡与通用余额对照')}
            className='min-w-[640px]'
          >
            <div
              role='row'
              className='border-foreground/20 grid grid-cols-[130px_minmax(0,1fr)_minmax(0,1fr)] border-b'
            >
              <div
                role='columnheader'
                className='codego-stat-label pr-3 pb-2.5'
              >
                {t('对比项目')}
              </div>
              <div
                role='columnheader'
                className='codego-stat-label pr-3 pb-2.5'
              >
                {t('月卡额度')}
              </div>
              <div role='columnheader' className='codego-stat-label pb-2.5'>
                {t('通用余额')}
              </div>
            </div>
            <div>
              {comparisonRows.map((row) => (
                <div
                  key={row.label}
                  role='row'
                  className='border-border/60 grid grid-cols-[130px_minmax(0,1fr)_minmax(0,1fr)] border-b last:border-b-0'
                >
                  <div
                    role='rowheader'
                    className='text-foreground py-3 pr-3 text-[13px] font-medium'
                  >
                    {t(row.label)}
                  </div>
                  <div
                    role='cell'
                    className='text-muted-foreground py-3 pr-3 text-[13px] tabular-nums'
                  >
                    {t(row.monthly)}
                  </div>
                  <div
                    role='cell'
                    className='text-muted-foreground py-3 text-[13px] tabular-nums'
                  >
                    {t(row.wallet)}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}
