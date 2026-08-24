import {
  ArrowRight,
  Compass,
  LineChart,
  Route,
  WalletCards,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

export function MarketplaceAudienceGuide(props: {
  onBrowse: () => void
  onManage: () => void
}) {
  const { t } = useTranslation()

  return (
    <section
      className='codego-marketplace-audience'
      aria-label={t('分组市场使用路径')}
    >
      <div className='codego-marketplace-audience-line' aria-hidden='true' />
      <div className='codego-marketplace-audience-copy'>
        <span
          className='codego-marketplace-audience-signal'
          aria-hidden='true'
        />
        <div>
          <p className='text-muted-foreground text-xs font-medium'>
            {t('同一个市场，两条清晰路径')}
          </p>
          <h3 className='mt-1 text-base font-semibold tracking-tight'>
            {t('先找到合适的分组，再决定如何长期使用')}
          </h3>
        </div>
      </div>
      <div className='codego-marketplace-audience-actions'>
        <Button variant='outline' size='sm' onClick={props.onBrowse}>
          <Compass />
          <span>
            <strong>{t('我是使用者')}</strong>
            <small>{t('比较倍率、速度和稳定性')}</small>
          </span>
          <ArrowRight className='codego-marketplace-audience-arrow' />
        </Button>
        <Button variant='outline' size='sm' onClick={props.onManage}>
          <WalletCards />
          <span>
            <strong>{t('我是渠道主')}</strong>
            <small>{t('查看收入、审核和服务健康')}</small>
          </span>
          <ArrowRight className='codego-marketplace-audience-arrow' />
        </Button>
      </div>
      <div className='text-muted-foreground hidden items-center gap-2 text-[11px] xl:flex'>
        <Route className='text-primary size-3.5' />
        <span>{t('市场数据每 30 秒自动更新')}</span>
        <LineChart className='text-primary ml-2 size-3.5' />
        <span>{t('排名基于实际观测样本')}</span>
      </div>
    </section>
  )
}
