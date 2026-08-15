import {
  BadgeDollarSign,
  Eye,
  Gauge,
  Network,
  ShieldCheck,
  Store,
  WalletCards,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatMultiplier } from '../lib/format'

export function MarketplaceOverview(props: {
  total: number
  ranked: number
  multiplier?: number
}) {
  const { t } = useTranslation()
  const metrics = [
    {
      icon: Store,
      label: t('可见分组'),
      value: String(props.total),
      hint: t('已通过公开条件'),
    },
    {
      icon: Gauge,
      label: t('正式排名'),
      value: String(props.ranked),
      hint: t('样本达到门槛'),
    },
    {
      icon: BadgeDollarSign,
      label: t('当前最低倍率'),
      value:
        props.multiplier == null
          ? '--'
          : `${formatMultiplier(props.multiplier)}x`,
      hint: t('按当前列表统计'),
    },
    {
      icon: Eye,
      label: t('公开规则'),
      value: t('审核前可见'),
      hint: t('来源审核后展示'),
    },
  ]

  return (
    <section className='border-border bg-card overflow-hidden rounded-xl border'>
      <div className='grid lg:grid-cols-[minmax(0,1.35fr)_minmax(300px,0.65fr)]'>
        <div className='px-5 py-6 sm:px-7 sm:py-7'>
          <div className='flex items-center gap-3'>
            <div className='bg-primary/10 text-primary flex size-10 items-center justify-center rounded-lg'>
              <Network className='size-5' />
            </div>
            <div>
              <h3 className='text-lg font-semibold text-balance sm:text-xl'>
                {t('找到更适合业务的模型通道')}
              </h3>
              <p className='text-muted-foreground mt-1 max-w-2xl text-sm leading-6 text-pretty'>
                {t(
                  '根据真实调用质量、首字速度和消费倍率比较第三方渠道，再绑定到指定 Token。'
                )}
              </p>
            </div>
          </div>
          <div className='mt-5 flex flex-wrap gap-x-5 gap-y-2 text-xs'>
            <span className='flex items-center gap-1.5'>
              <ShieldCheck className='text-success size-4' />
              {t('检测与管理员双重审核')}
            </span>
            <span className='flex items-center gap-1.5'>
              <Gauge className='text-info size-4' />
              {t('基于真实请求持续排名')}
            </span>
          </div>
        </div>
        <div className='border-border bg-primary/[0.045] flex items-start gap-3 border-t px-5 py-6 lg:border-t-0 lg:border-l lg:px-6'>
          <span className='bg-background text-primary flex size-10 shrink-0 items-center justify-center rounded-lg border'>
            <WalletCards className='size-5' />
          </span>
          <div>
            <div className='text-sm font-semibold'>{t('仅使用通用额度')}</div>
            <p className='text-muted-foreground mt-2 text-xs leading-5'>
              {t(
                '分组市场中的第三方分组，无论使用 GPT、Claude 或其他模型，调用时都只扣除通用额度。'
              )}
            </p>
          </div>
        </div>
      </div>
      <div className='border-border grid border-t sm:grid-cols-2 xl:grid-cols-4'>
        {metrics.map(({ icon: Icon, label, value, hint }) => (
          <div
            key={label}
            className='border-border flex min-h-24 items-center gap-3 border-b px-5 py-4 last:border-r-0 sm:border-r xl:border-b-0'
          >
            <Icon className='text-primary size-5 shrink-0' />
            <div className='min-w-0'>
              <div className='text-muted-foreground text-xs'>{label}</div>
              <div className='mt-1 truncate font-semibold tabular-nums'>
                {value}
              </div>
              <div className='text-muted-foreground mt-0.5 text-[11px]'>
                {hint}
              </div>
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}
