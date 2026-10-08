import { PageHeader, Tabs } from '../components/ui'
import { ModelsTab } from '../features/catalog/models-tab'
import { VendorsTab } from '../features/catalog/vendors-tab'
import { PrefillGroupsTab } from '../features/catalog/prefill-tab'

export default function ModelCatalogPage() {
  return (
    <>
      <PageHeader title="模型目录" />
      <Tabs
        items={[
          { value: 'models', label: '模型', content: <ModelsTab /> },
          { value: 'vendors', label: '厂商', content: <VendorsTab /> },
          { value: 'prefill', label: '预填分组', content: <PrefillGroupsTab /> },
        ]}
      />
    </>
  )
}
