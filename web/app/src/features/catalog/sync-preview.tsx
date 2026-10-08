// Sync upstream preview + apply for the 模型 tab. Fetches a diff via
// GET /api/catalog/models/sync_upstream/preview before letting the admin
// apply via POST /api/catalog/models/sync_upstream, matching the two-step
// flow in v3/internal/catalogcontrol/metadata_sync_preview.go and
// metadata_sync.go (preview is read-only; apply is a separate confirmed
// action, scoped per field via confirmAction).
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { useTranslation } from '../../lib/i18n'
import { Button, confirmAction, Drawer, ErrorMessage, Loading } from '../../components/ui'
import type { MetadataConflict } from './types'

type OverwriteField = 'description' | 'icon' | 'tags' | 'vendor' | 'name_rule' | 'status'

export function SyncPreviewAction() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [overwrite, setOverwrite] = useState<Record<string, OverwriteField[]>>({})
  const preview = useMutation({
    mutationFn: () =>
      api.GET('/api/catalog/models/sync_upstream/preview').then((result) => unwrap(result)),
    onSuccess: () => setOverwrite({}),
  })
  const apply = useMutation({
    mutationFn: () =>
      api
        .POST('/api/catalog/models/sync_upstream', {
          body: {
            overwrite: Object.entries(overwrite)
              .filter(([, fields]) => fields.length > 0)
              .map(([modelName, fields]) => ({ model_name: modelName, fields })),
          },
        })
        .then((result) => unwrap(result)),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['catalog-models'] })
    },
  })
  const toggleField = (model: string, field: OverwriteField) =>
    setOverwrite((prev) => {
      const current = prev[model] ?? []
      return {
        ...prev,
        [model]: current.includes(field)
          ? current.filter((item) => item !== field)
          : [...current, field],
      }
    })
  return (
    <>
      <Button
        variant="quiet"
        disabled={preview.isPending}
        onClick={() => {
          setOpen(true)
          preview.mutate()
        }}
      >
        {t('同步上游')}
      </Button>
      <Drawer open={open} onOpenChange={setOpen} title="上游同步预览">
        <ErrorMessage error={preview.error ?? apply.error} />
        {preview.isPending && <Loading />}
        {preview.data && (
          <div className="form-stack">
            <p>
              {t('缺失模型')}: {preview.data.missing.length} · {t('存在差异')}:{' '}
              {preview.data.conflicts.length}
            </p>
            {preview.data.missing.length > 0 && (
              <section className="section">
                <h3>{t('缺失模型（将在应用时创建）')}</h3>
                <ul>
                  {preview.data.missing.map((name) => (
                    <li key={name}>{name}</li>
                  ))}
                </ul>
              </section>
            )}
            {preview.data.conflicts.map((conflict: MetadataConflict) => (
              <section className="section" key={conflict.model_name}>
                <h3>{conflict.model_name}</h3>
                {(conflict.fields ?? []).map((field) => {
                  const key = field.field as OverwriteField
                  return (
                    <label className="checkbox-field" key={field.field}>
                      <input
                        type="checkbox"
                        checked={(overwrite[conflict.model_name] ?? []).includes(key)}
                        onChange={() => toggleField(conflict.model_name, key)}
                      />
                      {field.field}: {String(field.local)} → {String(field.upstream)}
                    </label>
                  )
                })}
              </section>
            ))}
            <Button
              disabled={apply.isPending}
              onClick={async () => {
                if (
                  await confirmAction({
                    title: '应用上游同步',
                    description: '将创建缺失模型并覆盖勾选的字段，此操作无法撤销。',
                    danger: true,
                  })
                )
                  apply.mutate()
              }}
            >
              {t('应用同步')}
            </Button>
            {apply.data && (
              <p role="status">
                {t('新建')} {apply.data.created_models} · {t('更新')} {apply.data.updated_models} ·{' '}
                {t('跳过')} {apply.data.skipped_models?.length ?? 0}
              </p>
            )}
          </div>
        )}
      </Drawer>
    </>
  )
}
