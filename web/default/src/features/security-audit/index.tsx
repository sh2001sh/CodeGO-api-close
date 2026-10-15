import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { SecurityAuditPanel } from './security-audit-panel'

export function SecurityAudit() {
  const { t } = useTranslation()
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('安全审计')}</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t('集中处理 Prompt Guard 与上游安全策略阻断事件')}
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <SecurityAuditPanel admin />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
