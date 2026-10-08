// 厂商 tab: vendor CRUD + search, backed by /api/catalog/vendors.
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useTranslation } from '../../lib/i18n'
import { DataTable } from '../../components/data-table'
import { Button, confirmAction, Drawer, ErrorMessage, Field, Loading } from '../../components/ui'
import type { VendorMetadata } from './types'

type VendorDraft = {
  id?: number | string | bigint
  name: string
  description: string
  icon: string
  status: number
}

function toDraft(vendor?: VendorMetadata): VendorDraft {
  return {
    id: vendor?.id,
    name: vendor?.name ?? '',
    description: vendor?.description ?? '',
    icon: vendor?.icon ?? '',
    status: vendor?.status ?? 1,
  }
}

export function VendorsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [keyword, setKeyword] = useState('')
  const [editing, setEditing] = useState<VendorDraft | null>(null)
  const vendors = useQuery(
    resourceOptions(
      'catalog-vendors',
      (signal) =>
        api
          .GET('/api/catalog/vendors', { signal, params: { query: { keyword, page_size: 100 } } })
          .then((result) => unwrap(result)),
      [keyword],
    ),
  )
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['catalog-vendors'] })
  const save = useMutation({
    mutationFn: (draft: VendorDraft) =>
      draft.id
        ? api.PUT('/api/catalog/vendors/{id}', {
            params: { path: { id: String(draft.id) } },
            body: draft,
          })
        : api.POST('/api/catalog/vendors', { body: draft }),
    onSuccess: () => {
      setEditing(null)
      void refresh()
    },
  })
  const remove = useMutation({
    mutationFn: (id: VendorMetadata['id']) =>
      api.DELETE('/api/catalog/vendors/{id}', { params: { path: { id: String(id) } } }),
    onSuccess: () => void refresh(),
  })
  return (
    <section className="section">
      <form
        className="filters"
        onSubmit={(event) => {
          event.preventDefault()
          setKeyword(String(new FormData(event.currentTarget).get('keyword') ?? ''))
        }}
      >
        <Field name="keyword" label="搜索厂商" placeholder="名称或描述" />
        <Button type="submit">{t('搜索')}</Button>
        <Button type="button" onClick={() => setEditing(toDraft())}>
          {t('创建')}
        </Button>
      </form>
      <ErrorMessage error={vendors.error ?? save.error ?? remove.error} />
      <Drawer
        open={editing !== null}
        onOpenChange={(open) => !open && setEditing(null)}
        title={editing?.id ? '编辑厂商' : '创建厂商'}
      >
        {editing && (
          <form
            className="form-panel"
            onSubmit={(event) => {
              event.preventDefault()
              const form = new FormData(event.currentTarget)
              save.mutate({
                id: editing.id,
                name: String(form.get('name') ?? ''),
                description: String(form.get('description') ?? ''),
                icon: String(form.get('icon') ?? ''),
                status: form.has('status') ? 1 : 0,
              })
            }}
          >
            <Field
              name="name"
              label="厂商名称"
              required
              defaultValue={editing.name}
              maxLength={128}
            />
            <Field name="description" label="描述" defaultValue={editing.description} />
            <Field name="icon" label="图标地址" defaultValue={editing.icon} />
            <label className="checkbox-field">
              <input type="checkbox" name="status" defaultChecked={editing.status !== 0} />
              {t('启用')}
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
      {vendors.isPending && <Loading />}
      {vendors.data && (
        <DataTable
          rows={vendors.data.items}
          rowKey={(row) => row.id}
          columns={[
            { label: '名称', render: (row) => row.name },
            { label: '描述', render: (row) => row.description || '—' },
            { label: '状态', render: (row) => t(row.status === 0 ? '停用' : '启用') },
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
                          title: '删除厂商',
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
