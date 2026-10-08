// 预填分组 tab: prefill_group CRUD, backed by /api/catalog/prefill-groups.
// Items must be a JSON array of strings for model/tag, or a JSON object or
// array for endpoint (see metadataSavePrefill/validPrefillItems in
// v3/internal/catalogcontrol/metadata_prefills.go).
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useTranslation } from '../../lib/i18n'
import { DataTable } from '../../components/data-table'
import {
  Button,
  confirmAction,
  Drawer,
  ErrorMessage,
  Field,
  Loading,
  SelectField,
} from '../../components/ui'
import type { PrefillGroup } from './types'

type Draft = {
  id?: number | string | bigint
  name: string
  type: 'model' | 'tag' | 'endpoint'
  description: string
  items: string
}

function toDraft(group?: PrefillGroup): Draft {
  return {
    id: group?.id,
    name: group?.name ?? '',
    type: (group?.type as Draft['type']) ?? 'model',
    description: group?.description ?? '',
    items: JSON.stringify(group?.items ?? [], null, 2),
  }
}

export function PrefillGroupsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<Draft | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const groups = useQuery(
    resourceOptions('catalog-prefill-groups', (signal) =>
      api.GET('/api/catalog/prefill-groups', { signal }).then((result) => unwrap(result)),
    ),
  )
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['catalog-prefill-groups'] })
  const save = useMutation({
    mutationFn: (draft: Omit<Draft, 'items'> & { items: Record<string, unknown> | unknown[] }) => {
      const body = {
        id: draft.id,
        name: draft.name,
        type: draft.type,
        description: draft.description,
        items: draft.items,
      }
      return draft.id
        ? api.PUT('/api/catalog/prefill-groups/{id}', {
            params: { path: { id: String(draft.id) } },
            body,
          })
        : api.POST('/api/catalog/prefill-groups', { body })
    },
    onSuccess: () => {
      setEditing(null)
      void refresh()
    },
  })
  const remove = useMutation({
    mutationFn: (id: PrefillGroup['id']) =>
      api.DELETE('/api/catalog/prefill-groups/{id}', { params: { path: { id: String(id) } } }),
    onSuccess: () => void refresh(),
  })
  return (
    <section className="section">
      <div className="filters">
        <Button onClick={() => setEditing(toDraft())}>{t('创建')}</Button>
      </div>
      <ErrorMessage error={error ?? groups.error ?? save.error ?? remove.error} />
      <Drawer
        open={editing !== null}
        onOpenChange={(open) => !open && setEditing(null)}
        title={editing?.id ? '编辑预填分组' : '创建预填分组'}
      >
        {editing && (
          <form
            className="form-panel"
            onSubmit={(event) => {
              event.preventDefault()
              setError(null)
              const form = new FormData(event.currentTarget)
              try {
                const items = JSON.parse(String(form.get('items') ?? '[]'))
                const type = String(form.get('type') ?? 'model')
                if (type !== 'model' && type !== 'tag' && type !== 'endpoint')
                  throw new Error('无效的预填类型')
                save.mutate({
                  id: editing.id,
                  name: String(form.get('name') ?? ''),
                  type,
                  description: String(form.get('description') ?? ''),
                  items,
                })
              } catch (failure) {
                setError(
                  failure instanceof Error && failure.message === '无效的预填类型'
                    ? failure
                    : new Error('items 必须为有效 JSON'),
                )
              }
            }}
          >
            <Field name="name" label="名称" required defaultValue={editing.name} maxLength={128} />
            <SelectField
              name="type"
              label="类型"
              defaultValue={editing.type}
              options={[
                { value: 'model', label: '模型' },
                { value: 'tag', label: '标签' },
                { value: 'endpoint', label: '端点' },
              ]}
            />
            <Field name="description" label="描述" defaultValue={editing.description} />
            <label className="field full-width" style={{ flexBasis: '100%' }} htmlFor="items">
              <span>{t('内容 (JSON)')}</span>
              <textarea id="items" name="items" rows={6} defaultValue={editing.items} />
            </label>
            <Button type="submit" disabled={save.isPending}>
              {t('保存')}
            </Button>
            <Button type="button" variant="quiet" onClick={() => setEditing(null)}>
              {t('取消')}
            </Button>
          </form>
        )}
      </Drawer>
      {groups.isPending && <Loading />}
      {groups.data && (
        <DataTable
          rows={groups.data}
          rowKey={(row) => row.id}
          columns={[
            { label: '名称', render: (row) => row.name },
            { label: '类型', render: (row) => row.type },
            { label: '描述', render: (row) => row.description || '—' },
            {
              label: '操作',
              render: (row) => (
                <div className="row-actions">
                  <Button variant="quiet" onClick={() => setEditing(toDraft(row))}>
                    {t('编辑')}
                  </Button>
                  <Button
                    variant="danger"
                    disabled={remove.isPending}
                    onClick={async () => {
                      if (
                        await confirmAction({
                          title: '删除预填分组',
                          description: '删除后无法恢复，确认删除？',
                          danger: true,
                        })
                      )
                        remove.mutate(row.id)
                    }}
                  >
                    {t('删除')}
                  </Button>
                </div>
              ),
            },
          ]}
        />
      )}
    </section>
  )
}
