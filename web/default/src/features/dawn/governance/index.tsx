import { useTranslation } from 'react-i18next'
import { SiteSeo } from '@/components/seo'
import { AdminGovernance } from '@/features/marketplace/components/admin-governance'

export function DawnMarketplaceGovernance() {
  const { t } = useTranslation()
  return (
    <div className='dawn dawn-governance'>
      <SiteSeo
        title={t('市场分组治理')}
        description={t('管理市场渠道、渠道主与收益回收')}
        canonicalPath='/marketplace'
      />
      <div className='mhead'>
        <div>
          <div className='kick'>A·01 MARKET GROUPS</div>
          <h1>{t('市场分组治理')}</h1>
        </div>
      </div>
      <AdminGovernance />
    </div>
  )
}
