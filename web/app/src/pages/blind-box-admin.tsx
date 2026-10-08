import { PageHeader, Tabs } from '../components/ui'
import { PoolEditor } from '../features/commerce-admin/pool-editor'
import { UserGrants } from '../features/commerce-admin/user-grants'

export default function BlindBoxAdminPage() {
  return (
    <>
      <PageHeader
        title="盲盒管理"
        description="配置盲盒池概率与限购，并为单个用户发放或撤销盲盒。"
      />
      <Tabs
        label="盲盒管理"
        items={[
          { value: 'pools', label: '池配置', content: <PoolEditor /> },
          { value: 'grants', label: '用户发放', content: <UserGrants /> },
        ]}
      />
    </>
  )
}
