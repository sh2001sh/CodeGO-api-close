// 模型 tab: catalog model list/search/CRUD, missing models and sync upstream
// with preview, backed by /api/catalog/models(/missing|/search|/sync_upstream*).
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
import { SyncPreviewAction } from './sync-preview'
import type { ModelView } from './types'

type Draft = {
  id?: number | string | bigint
  model_name: string
  description: string
  icon: string
  tags: string
  vendor_id: string
  status: number
  sync_official: number
  name_rule: number
}

function toDraft(model?: ModelView): Draft {
  return {
    id: model?.id,
    model_name: model?.model_name ?? '',
    description: model?.description ?? '',
    icon: model?.icon ?? '',
    tags: model?.tags ?? '',
    vendor_id: model?.vendor_id ? String(model.vendor_id) : '',
    status: model?.status ?? 1,
    sync_official: model?.sync_official ?? 0,
    name_rule: model?.name_rule ?? 0,
  }
}

export function ModelsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [keyword, setKeyword] = useState('')
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<Draft | null>(null)
  const models = useQuery(
    resourceOptions(
      'catalog-models',
      (signal) =>
        api
          .GET('/api/catalog/models', {
            signal,
            params: { query: { keyword, page, page_size: 30 } },
          })
          .then((result) => unwrap(result)),
      [keyword, page],
    ),
  )
  const missing = useQuery(
    resourceOptions('catalog-models-missing', (signal) =>
      api.GET('/api/catalog/models/missing', { signal }).then((result) => unwrap(result)),
    ),
  )
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['catalog-models'] })
    void queryClient.invalidateQueries({ queryKey: ['catalog-models-missing'] })
  }
  const save = useMutation({
    mutationFn: (draft: Draft) => {
      const body = {
        id: draft.id,
        model_name: draft.model_name,
        description: draft.description,
        icon: draft.icon,
        tags: draft.tags,
        vendor_id: draft.vendor_id ? Number(draft.vendor_id) : undefined,
        status: draft.status,
        sync_official: draft.sync_official,
        name_rule: draft.name_rule,
      }
      return draft.id
        ? api.PUT('/api/catalog/models/{id}', { params: { path: { id: String(draft.id) } }, body })
        : api.POST('/api/catalog/models', { body })
    },
    onSuccess: () => {
      setEditing(null)
      refresh()
    },
  })
  const remove = useMutation({
    mutationFn: (id: ModelView['id']) =>
      api.DELETE('/api/catalog/models/{id}', { params: { path: { id: String(id) } } }),
    onSuccess: refresh,
  })
  return (
    <section className="section">
      <div className="filters">
        <form
          className="filters"
          onSubmit={(event) => {
            event.preventDefault()
            setKeyword(String(new FormData(event.currentTarget).get('keyword') ?? ''))
            setPage(1)
          }}
        >
          <Field name="keyword" label="搜索模型" placeholder="模型名称或描述" />
          <Button type="submit">{t('搜索')}</Button>
        </form>
        <Button onClick={() => setEditing(toDraft())}>{t('创建')}</Button>
        <SyncPreviewAction />
      </div>
      {missing.data && missing.data.length > 0 && (
        <p className="notice" role="status">
          {t('有渠道提供但未在目录登记的模型')}: {missing.data.join(', ')}
        </p>
      )}
      <ErrorMessage error={models.error ?? missing.error ?? save.error ?? remove.error} />
      <Drawer
        open={editing !== null}
        onOpenChange={(open) => !open && setEditing(null)}
        title={editing?.id ? '编辑模型' : '创建模型'}
      >
        {editing && (
          <form
            className="form-panel"
            onSubmit={(event) => {
              event.preventDefault()
              const form = new FormData(event.currentTarget)
              save.mutate({
                id: editing.id,
                model_name: String(form.get('model_name') ?? ''),
                description: String(form.get('description') ?? ''),
                icon: String(form.get('icon') ?? ''),
                tags: String(form.get('tags') ?? ''),
                vendor_id: String(form.get('vendor_id') ?? ''),
                status: form.has('status') ? 1 : 0,
                sync_official: form.has('sync_official') ? 1 : 0,
                name_rule: Number(form.get('name_rule') ?? 0),
              })
            }}
          >
            <Field name="model_name" label="模型名称" required defaultValue={editing.model_name} />
            <Field name="description" label="描述" defaultValue={editing.description} />
            <Field name="icon" label="图标地址" defaultValue={editing.icon} />
            <Field name="tags" label="标签（逗号分隔）" defaultValue={editing.tags} />
            <Field
              name="vendor_id"
              label="厂商编号"
              type="number"
              min={1}
              defaultValue={editing.vendor_id}
            />
            <SelectField
              name="name_rule"
              label="匹配方式"
              defaultValue={String(editing.name_rule)}
              options={[
                { value: '0', label: '精确匹配' },
                { value: '1', label: '前缀匹配' },
                { value: '2', label: '包含匹配' },
                { value: '3', label: '后缀匹配' },
              ]}
            />
            <label className="checkbox-field">
              <input type="checkbox" name="status" defaultChecked={editing.status !== 0} />
              {t('启用')}
            </label>
            <label className="checkbox-field">
              <input
                type="checkbox"
                name="sync_official"
                defaultChecked={editing.sync_official !== 0}
              />
              {t('跟随官方同步')}
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
      {models.isPending && <Loading />}
      {models.data && (
        <>
          <DataTable
            rows={models.data.items}
            rowKey={(row) => row.id}
            columns={[
              { label: '模型名称', render: (row) => row.model_name },
              { label: '标签', render: (row) => row.tags || '—' },
              {
                label: '绑定渠道数',
                render: (row) => row.bound_channels?.length ?? 0,
                numeric: true,
              },
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
                            title: '删除模型',
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
          <div className="row-actions">
            <Button variant="quiet" disabled={page === 1} onClick={() => setPage(page - 1)}>
              {t('上一页')}
            </Button>
            <Button
              variant="quiet"
              disabled={page * 30 >= Number(models.data.total)}
              onClick={() => setPage(page + 1)}
            >
              {t('下一页')}
            </Button>
          </div>
        </>
      )}
    </section>
  )
}
