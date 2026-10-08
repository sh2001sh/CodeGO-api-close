// Advanced / raw editor: covers settings keys returned by GET /api/settings
// that have no entry in the typed registry, plus create and delete. This is
// the only place create/delete happen; registered keys are edited through
// their typed group form instead.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { useTranslation } from '../../lib/i18n'
import { DataTable } from '../../components/data-table'
import {
  Button,
  confirmAction,
  ErrorMessage,
  Field,
  Panel,
  TextAreaField,
} from '../../components/ui'
import type { Schema } from '../../lib/types'
import { fieldByKey } from './registry'

type Setting = Schema['CatalogSetting']

export function RawSettingsEditor(props: { settings: readonly Setting[] }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const unregistered = props.settings.filter((item) => !fieldByKey(item.key))
  const [editing, setEditing] = useState<Setting | 'new' | null>(null)
  const [localError, setLocalError] = useState<Error | null>(null)

  const save = useMutation({
    mutationFn: (setting: Setting) =>
      api.PUT('/api/settings/{key}', {
        params: { path: { key: setting.key } },
        body: { value: setting.value, sensitive: setting.sensitive },
      }),
    onSuccess: () => {
      setEditing(null)
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
  })
  const remove = useMutation({
    mutationFn: (key: string) => api.DELETE('/api/settings/{key}', { params: { path: { key } } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['settings'] }),
  })

  const setting = editing === 'new' ? undefined : editing
  return (
    <Panel
      title="高级 / 原始配置"
      description="覆盖未纳入分组表单的配置键；支持新增与删除，删除后无法恢复。"
      action={<Button onClick={() => setEditing('new')}>{t('创建')}</Button>}
    >
      <ErrorMessage error={save.error ?? remove.error ?? localError} />
      {editing && (
        <form
          key={setting?.key ?? 'new'}
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setLocalError(null)
            const form = new FormData(event.currentTarget)
            try {
              save.mutate({
                key: String(form.get('key')),
                value: JSON.parse(String(form.get('value'))),
                sensitive: form.has('sensitive'),
                configured: true,
              })
            } catch {
              setLocalError(new Error('配置值必须为有效 JSON'))
            }
          }}
        >
          <Field
            label="设置键"
            name="key"
            required
            defaultValue={setting?.key}
            readOnly={!!setting}
          />
          <TextAreaField
            name="value"
            label="配置值"
            required
            rows={4}
            defaultValue={setting?.sensitive ? '' : JSON.stringify(setting?.value ?? '', null, 2)}
          />
          <label className="checkbox-field">
            <input type="checkbox" name="sensitive" defaultChecked={setting?.sensitive} />
            {t('敏感配置')}
          </label>
          <div className="form-actions">
            <Button disabled={save.isPending} type="submit">
              {t('保存')}
            </Button>
            <Button type="button" variant="quiet" onClick={() => setEditing(null)}>
              {t('取消')}
            </Button>
          </div>
        </form>
      )}
      <DataTable
        rows={unregistered}
        rowKey={(row) => row.key}
        empty="没有需要在此处理的原始配置键"
        columns={[
          { label: '设置键', render: (row) => row.key },
          {
            label: '配置值',
            render: (row) =>
              row.sensitive ? t('已加密') : <code>{JSON.stringify(row.value)}</code>,
          },
          {
            label: '操作',
            render: (row) => (
              <div className="row-actions">
                <Button variant="quiet" onClick={() => setEditing(row)}>
                  {t('编辑')}
                </Button>
                <Button
                  variant="danger"
                  disabled={remove.isPending}
                  onClick={async () => {
                    if (
                      await confirmAction({
                        title: '删除后无法恢复，确认删除？',
                        danger: true,
                      })
                    )
                      remove.mutate(row.key)
                  }}
                >
                  {t('删除')}
                </Button>
              </div>
            ),
          },
        ]}
      />
    </Panel>
  )
}
