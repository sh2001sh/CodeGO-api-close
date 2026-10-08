import { useTranslation } from '../../lib/i18n'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { Button, ErrorMessage, Field, confirmAction } from '../../components/ui'
import { MarketForm, text } from './form'

export function ModelRecovery(props: { id: string; failed: boolean }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const params = { path: { id: props.id } }
  const refresh = () => client.invalidateQueries({ queryKey: ['market-mine'] })
  const retry = useMutation({
    mutationFn: () => api.POST('/api/marketplace/channels/{id}/test/failed', { params }),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (model: string) =>
      api.POST('/api/marketplace/channels/{id}/models/remove-failed', { params, body: { model } }),
    onSuccess: refresh,
  })
  return (
    <details className="section">
      <summary>{t('失败模型处理')}</summary>
      <p className="muted">{t('移除仅适用于已有失败验证记录的模型，服务端会检查资格。')}</p>
      <ErrorMessage error={retry.error ?? remove.error} />
      <Button
        variant="quiet"
        disabled={retry.isPending || !props.failed}
        onClick={() => retry.mutate()}
      >
        {t('重试验证')}
      </Button>
      <MarketForm
        pending={remove.isPending}
        submit="移除失败模型"
        onSubmit={(fields) => {
          const model = text(fields, 'failed-model')
          void confirmAction({
            title: `${t('确认移除失败模型「')}${model}${t('」？')}`,
            danger: true,
          }).then((ok) => {
            if (ok) remove.mutate(model)
          })
        }}
      >
        <Field name="failed-model" label="失败模型名称" required />
      </MarketForm>
    </details>
  )
}
