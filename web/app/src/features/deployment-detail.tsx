import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { Button, ErrorMessage, Field, Loading } from '../components/ui'
import { DataTable } from '../components/data-table'

export function DeploymentDetail(props: { id: string; onClose: () => void }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const detail = useQuery(
    resourceOptions(
      'deployment-detail',
      (signal) =>
        api
          .GET('/api/deployments/{id}', { signal, params: { path: { id: props.id } } })
          .then(unwrap),
      [props.id],
    ),
  )
  const containers = useQuery(
    resourceOptions(
      'deployment-containers',
      (signal) =>
        api
          .GET('/api/deployments/{id}/containers', { signal, params: { path: { id: props.id } } })
          .then(unwrap),
      [props.id],
    ),
  )
  const [containerID, setContainerID] = useState('')
  const logs = useQuery({
    ...resourceOptions(
      'deployment-logs',
      (signal) =>
        api
          .GET('/api/deployments/{id}/logs', {
            signal,
            params: { path: { id: props.id }, query: { container_id: containerID, limit: 100 } },
          })
          .then(unwrap),
      [props.id, containerID],
    ),
    enabled: !!containerID,
  })
  const [deleting, setDeleting] = useState(false)
  const refreshed = () => {
    void client.invalidateQueries({ queryKey: ['deployments'] })
    void detail.refetch()
    void containers.refetch()
  }
  const rename = useMutation({
    mutationFn: (name: string) =>
      api.PUT('/api/deployments/{id}/name', { params: { path: { id: props.id } }, body: { name } }),
    onSuccess: refreshed,
  })
  const extend = useMutation({
    mutationFn: (duration_hours: number) =>
      api.POST('/api/deployments/{id}/extend', {
        params: { path: { id: props.id } },
        body: { duration_hours },
      }),
    onSuccess: refreshed,
  })
  const update = useMutation({
    mutationFn: (body: { image_url: string; traffic_port: number }) =>
      api.PUT('/api/deployments/{id}', { params: { path: { id: props.id } }, body }),
    onSuccess: refreshed,
  })
  const remove = useMutation({
    mutationFn: () => api.DELETE('/api/deployments/{id}', { params: { path: { id: props.id } } }),
    onSuccess: () => {
      refreshed()
      props.onClose()
    },
  })
  return (
    <section className="section">
      <h2>
        {t('部署')} {props.id}
      </h2>
      <Button variant="quiet" onClick={props.onClose}>
        {t('关闭部署详情')}
      </Button>
      <ErrorMessage
        error={
          detail.error ??
          containers.error ??
          logs.error ??
          rename.error ??
          extend.error ??
          update.error ??
          remove.error
        }
      />
      {detail.isPending && <Loading />}
      {detail.data && (
        <>
          <dl className="metrics">
            <div>
              <dt>{t('状态')}</dt>
              <dd>{detail.data.status}</dd>
            </div>
            <div>
              <dt>{t('硬件')}</dt>
              <dd>{detail.data.hardware_name}</dd>
            </div>
            <div>
              <dt>{t('容器数量')}</dt>
              <dd>{detail.data.total_containers}</dd>
            </div>
            <div>
              <dt>{t('剩余分钟')}</dt>
              <dd>{detail.data.compute_minutes_remaining}</dd>
            </div>
          </dl>
          <form
            className="form-panel"
            onSubmit={(event) => {
              event.preventDefault()
              rename.mutate(String(new FormData(event.currentTarget).get('rename')))
            }}
          >
            <Field name="rename" label="新部署名称" required maxLength={255} />
            <Button type="submit" disabled={rename.isPending}>
              {t('修改名称')}
            </Button>
          </form>
          <form
            className="form-panel"
            onSubmit={(event) => {
              event.preventDefault()
              extend.mutate(Number(new FormData(event.currentTarget).get('extend-hours')))
            }}
          >
            <Field name="extend-hours" label="延长小时数" type="number" min={1} required />
            <p>{t('延长运行会产生外部资源费用。')}</p>
            <Button type="submit" disabled={extend.isPending}>
              {t('确认付费延长')}
            </Button>
          </form>
          <form
            className="form-panel"
            onSubmit={(event) => {
              event.preventDefault()
              const form = new FormData(event.currentTarget)
              update.mutate({
                image_url: String(form.get('update-image')),
                traffic_port: Number(form.get('update-port')),
              })
            }}
          >
            <Field
              name="update-image"
              label="容器镜像"
              required
              defaultValue={detail.data.container_config.image_url}
            />
            <Field
              name="update-port"
              label="服务端口"
              type="number"
              min={1}
              required
              defaultValue={detail.data.container_config.traffic_port}
            />
            <Button type="submit" disabled={update.isPending}>
              {t('更新部署配置')}
            </Button>
          </form>
        </>
      )}
      {containers.data && (
        <DataTable
          rows={containers.data.containers}
          rowKey={(item) => item.container_id}
          columns={[
            { label: '容器编号', render: (item) => item.container_id },
            { label: '状态', render: (item) => item.status },
            { label: '硬件', render: (item) => item.hardware },
            {
              label: '操作',
              render: (item) => (
                <Button variant="quiet" onClick={() => setContainerID(item.container_id)}>
                  {t('查看容器日志')}
                </Button>
              ),
            },
          ]}
        />
      )}
      {logs.isSuccess && (
        <div className="section">
          <h3>{t('容器日志')}</h3>
          {logs.data ? (
            <pre>{logs.data}</pre>
          ) : (
            <p className="empty-state">{t('暂无容器日志。')}</p>
          )}
        </div>
      )}
      <Button variant="danger" onClick={() => setDeleting(true)}>
        {t('删除外部部署')}
      </Button>
      {deleting && (
        <div className="form-panel">
          <p>
            {t('确认删除外部部署')} {props.id}
            {t('？此操作会停止外部资源，不能撤销。')}
          </p>
          <Button variant="danger" disabled={remove.isPending} onClick={() => remove.mutate()}>
            {t('确认删除部署')}
          </Button>
          <Button variant="quiet" disabled={remove.isPending} onClick={() => setDeleting(false)}>
            {t('取消')}
          </Button>
        </div>
      )}
    </section>
  )
}
