import { useSuspenseQuery } from '@tanstack/react-query'
import { sessionOptions } from '../lib/queries'
import { PageHeader, Tabs } from '../components/ui'
import { AuditRequestsTab } from '../features/analytics/audit-requests'
import { AuditEventsTab } from '../features/analytics/audit-events'
import { AuditUsageTab } from '../features/analytics/audit-usage'

export default function AuditPage() {
  const user = useSuspenseQuery(sessionOptions()).data
  const isAdmin = user.role === 'admin' || user.role === 'root'
  return (
    <>
      <PageHeader title="请求审计" description="追踪请求、事件与用量的历史记录" />
      <Tabs
        items={[
          { value: 'requests', label: '请求', content: <AuditRequestsTab isAdmin={isAdmin} /> },
          { value: 'events', label: '事件', content: <AuditEventsTab isAdmin={isAdmin} /> },
          { value: 'usage', label: '用量', content: <AuditUsageTab isAdmin={isAdmin} /> },
        ]}
      />
    </>
  )
}
