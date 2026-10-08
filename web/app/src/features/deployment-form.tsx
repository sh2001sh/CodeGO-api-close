import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { Button, ErrorMessage, Field } from '../components/ui'

export function DeploymentForm() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [prepared, setPrepared] = useState<Schema['DeploymentDeploymentRequest'] | null>(null)
  const estimate = useMutation({
    mutationFn: (body: Schema['DeploymentPriceEstimationRequest']) =>
      api.POST('/api/deployments/price-estimation', { body }).then(unwrap),
  })
  const create = useMutation({
    mutationFn: (body: Schema['DeploymentDeploymentRequest']) =>
      api.POST('/api/deployments/', { body }).then(unwrap),
    onSuccess: () => {
      setPrepared(null)
      void client.invalidateQueries({ queryKey: ['deployments'] })
    },
  })
  return (
    <section className="section">
      <h2>{t('创建模型部署')}</h2>
      <p>{t('创建部署会向外部服务提交付费资源申请。先估价，再核对费用后创建。')}</p>
      <ErrorMessage error={estimate.error ?? create.error} />
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          create.reset()
          const form = new FormData(event.currentTarget)
          const body = {
            resource_private_name: String(form.get('deployment-name')),
            hardware_id: Number(form.get('hardware-id')),
            location_ids: String(form.get('location-ids'))
              .split(/[\s,，]+/)
              .filter(Boolean)
              .map(Number),
            duration_hours: Number(form.get('duration-hours')),
            gpus_per_container: Number(form.get('gpus')),
            container_config: {
              replica_count: Number(form.get('replicas')),
              traffic_port: Number(form.get('port')) || undefined,
            },
            registry_config: { image_url: String(form.get('image')) },
          }
          setPrepared(body)
          estimate.mutate({
            hardware_id: body.hardware_id,
            location_ids: body.location_ids,
            duration_hours: body.duration_hours,
            gpus_per_container: body.gpus_per_container,
            replica_count: body.container_config.replica_count,
            currency: 'usd',
            duration_type: 'hour',
            duration_qty: body.duration_hours,
            hardware_qty: body.gpus_per_container,
          })
        }}
      >
        <Field name="deployment-name" label="部署名称" required maxLength={255} />
        <Field name="hardware-id" label="硬件编号" type="number" min={1} required />
        <Field name="location-ids" label="地区编号（逗号分隔）" required />
        <Field
          name="duration-hours"
          label="运行小时数"
          type="number"
          min={1}
          required
          defaultValue={1}
        />
        <Field
          name="gpus"
          label="每容器 GPU 数量"
          type="number"
          min={1}
          required
          defaultValue={1}
        />
        <Field name="replicas" label="副本数" type="number" min={1} required defaultValue={1} />
        <Field name="port" label="服务端口" type="number" min={1} defaultValue={8000} />
        <Field
          name="image"
          label="容器镜像"
          required
          placeholder="registry.example/model:version"
        />
        <Button type="submit" disabled={estimate.isPending || create.isPending}>
          {estimate.isPending ? t('估价中…') : t('获取部署估价')}
        </Button>
      </form>
      {prepared && estimate.data && (
        <div className="form-panel">
          <p>
            {t('部署“')}
            {prepared.resource_private_name}” · {prepared.duration_hours} {t('小时 ·')}{' '}
            {prepared.container_config.replica_count} {t('个副本 · 预计')}{' '}
            {estimate.data.estimated_cost} {estimate.data.currency.toUpperCase()}
          </p>
          <Button
            disabled={create.isPending || !estimate.data.estimation_valid}
            onClick={() => create.mutate(prepared)}
          >
            {create.isPending ? t('创建中…') : t('确认费用并创建部署')}
          </Button>
          <Button
            variant="quiet"
            disabled={create.isPending}
            onClick={() => {
              setPrepared(null)
              estimate.reset()
            }}
          >
            {t('取消创建')}
          </Button>
        </div>
      )}
      {create.data && (
        <p role="status">
          {t('外部服务已接受部署')} {create.data.deployment_id} · {create.data.status}
        </p>
      )}
    </section>
  )
}
