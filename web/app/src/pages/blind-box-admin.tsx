import { PageHeader, Tabs } from '../components/ui'
import { PoolEditor } from '../features/commerce-admin/pool-editor'
import { UserGrants } from '../features/commerce-admin/user-grants'
import { BoxBatchEditor } from '../features/commerce-admin/batch-editor'

export default function BlindBoxAdminPage() {
  return (
    <>
      <PageHeader
        title="盲盒管理"
        description="发布固定数量的回馈批次、锁定履约准备金，并保留旧版奖池与库存管理。"
      />
      <Tabs
        label="盲盒管理"
        items={[
          { value: 'batches', label: '批次与准备金', content: <BoxBatchEditor /> },
          { value: 'pools', label: '旧版池配置', content: <PoolEditor /> },
          { value: 'grants', label: '用户发放', content: <UserGrants /> },
        ]}
      />
    </>
  )
}
