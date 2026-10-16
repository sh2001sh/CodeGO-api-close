import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import '@/features/channels/admin-workspace.css'
import { AdminGovernance } from '@/features/marketplace/components/admin-governance'
import '@/features/marketplace/marketplace-admin.css'

export function DawnMarketplaceGovernance() {
  const { t } = useTranslation()
  return (
    <div className='admin-workspace marketplace-governance-workspace'>
      <SiteSeo
        title={t('市场分组治理')}
        description={t('管理市场渠道、渠道主与收益回收')}
        canonicalPath='/marketplace'
      />
      <SectionPageLayout kicker=''>
        <SectionPageLayout.Title>{t('市场分组治理')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <AdminGovernance />
        </SectionPageLayout.Content>
      </SectionPageLayout>
    </div>
  )
}
