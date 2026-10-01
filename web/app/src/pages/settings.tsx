import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, PageHeader } from '../components/ui'

type Setting = Schema['CatalogSetting']

export default function SettingsPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data } = useSuspenseQuery(
    resourceOptions('settings', (signal) =>
      api.GET('/api/settings', { signal }).then((result) => unwrap(result)),
    ),
  )
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
  const setting = editing === 'new' ? undefined : editing
  return (
    <>
      <PageHeader
        title="系统设置"
        action={<Button onClick={() => setEditing('new')}>{t('创建')}</Button>}
      />
      <ErrorMessage error={save.error ?? localError} />
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
          <Field label="设置键" name="key" required defaultValue={setting?.key} />
          <label className="field" htmlFor="setting-value">
            <span>{t('配置值')}</span>
            <textarea
              id="setting-value"
              name="value"
              required
              rows={4}
              defaultValue={setting?.sensitive ? '' : JSON.stringify(setting?.value ?? '', null, 2)}
            />
          </label>
          <label>
            <input type="checkbox" name="sensitive" defaultChecked={setting?.sensitive} />{' '}
            {t('敏感配置')}
          </label>
          <Button disabled={save.isPending} type="submit">
            {t('保存')}
          </Button>
          <Button type="button" variant="quiet" onClick={() => setEditing(null)}>
            {t('取消')}
          </Button>
        </form>
      )}
      <DataTable
        rows={data ?? []}
        rowKey={(row) => row.key}
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
              <Button variant="quiet" onClick={() => setEditing(row)}>
                {t('编辑')}
              </Button>
            ),
          },
        ]}
      />
    </>
  )
}
