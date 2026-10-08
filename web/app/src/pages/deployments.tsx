import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { date } from '../lib/format'
import { Button, ErrorMessage, Loading, PageHeader } from '../components/ui'
import { DataTable } from '../components/data-table'
import { DeploymentForm } from '../features/deployment-form'
import { DeploymentDetail } from '../features/deployment-detail'

export default function DeploymentsPage() {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [id, setID] = useState('')
  const settings = useQuery(
    resourceOptions('deployment-settings', (signal) =>
      api.GET('/api/deployments/settings', { signal }).then(unwrap),
    ),
  )
  const enabled = settings.data?.enabled === true && settings.data.configured === true
  const deployments = useQuery({
    ...resourceOptions(
      'deployments',
      (signal) =>
        api
          .GET('/api/deployments/', { signal, params: { query: { page, page_size: 20 } } })
          .then(unwrap),
      [page],
    ),
    enabled,
  })
  const hardware = useQuery({
    ...resourceOptions('deployment-hardware', (signal) =>
      api.GET('/api/deployments/hardware-types', { signal }).then(unwrap),
    ),
    enabled,
  })
  const locations = useQuery({
    ...resourceOptions('deployment-locations', (signal) =>
      api.GET('/api/deployments/locations', { signal }).then(unwrap),
    ),
    enabled,
  })
  const connect = useMutation({
    mutationFn: () =>
      api.POST('/api/deployments/settings/test-connection', { body: {} }).then(unwrap),
  })
  return (
    <>
      <PageHeader
        title="模型部署"
        action={
          <Button
            variant="quiet"
            disabled={connect.isPending || !enabled}
            onClick={() => connect.mutate()}
          >
            {t('测试外部服务连接')}
          </Button>
        }
      />
      <ErrorMessage
        error={
          settings.error ?? deployments.error ?? hardware.error ?? locations.error ?? connect.error
        }
      />
      {settings.isPending && <Loading />}
      {settings.data && !enabled && (
        <p className="notice">
          {t('外部部署服务尚未启用或未配置凭据。超级管理员可在系统设置中配置 io.net 服务。')}
        </p>
      )}
      {connect.isSuccess && <p role="status">{t('连接测试已完成。')}</p>}
      {enabled && (
        <>
          {deployments.isPending && <Loading />}
          {deployments.data && (
            <>
              <DataTable
                rows={deployments.data.items}
                rowKey={(item) => item.id}
                columns={[
                  { label: '名称', render: (item) => item.deployment_name },
                  { label: '状态', render: (item) => item.status },
                  { label: '硬件', render: (item) => item.hardware_name },
                  { label: '创建时间', render: (item) => date(Number(item.created_at) * 1000) },
                  {
                    label: '操作',
                    render: (item) => (
                      <Button variant="quiet" onClick={() => setID(item.id)}>
                        {t('查看与管理')}
                      </Button>
                    ),
                  },
                ]}
              />
              <div className="row-actions">
                <Button variant="quiet" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                  {t('上一页')}
                </Button>
                <Button
                  variant="quiet"
                  disabled={page * 20 >= deployments.data.total}
                  onClick={() => setPage(page + 1)}
                >
                  {t('下一页')}
                </Button>
              </div>
            </>
          )}
          {id && <DeploymentDetail key={id} id={id} onClose={() => setID('')} />}
          {hardware.data && (
            <details className="section">
              <summary>{t('可用硬件编号')}</summary>
              <DataTable
                rows={hardware.data.hardware_types}
                rowKey={(item) => item.id}
                columns={[
                  { label: '编号', render: (item) => item.id },
                  { label: '名称', render: (item) => item.name },
                  { label: '显存（GB）', render: (item) => item.gpu_memory },
                  { label: '可用', render: (item) => (item.available ? t('是') : t('否')) },
                  { label: '小时价格', render: (item) => item.hourly_rate },
                ]}
              />
            </details>
          )}
          {locations.data && (
            <details className="section">
              <summary>{t('可用地区编号')}</summary>
              <DataTable
                rows={locations.data.locations}
                rowKey={(item) => item.id}
                columns={[
                  { label: '编号', render: (item) => item.id },
                  { label: '名称', render: (item) => item.name },
                  {
                    label: '地区',
                    render: (item) => item.region || item.country || item.iso2 || '—',
                  },
                ]}
              />
            </details>
          )}
          <DeploymentForm />
        </>
      )}
    </>
  )
}
