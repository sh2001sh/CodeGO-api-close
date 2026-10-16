import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import '@/features/channels/admin-workspace.css'
import '@/features/users/admin.css'
import { SecurityAuditPanel } from './security-audit-panel'

export function SecurityAudit() {
  const { t } = useTranslation()
  return (
    <div className='admin-workspace solaris-admin'>
      <SectionPageLayout kicker=''>
        <SectionPageLayout.Title>{t('安全审计')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          <SecurityAuditPanel admin />
        </SectionPageLayout.Content>
      </SectionPageLayout>
    </div>
  )
}
