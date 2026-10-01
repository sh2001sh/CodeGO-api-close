import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { Copy, Plus, Trash2 } from 'lucide-react'
import { api, unwrap } from '../lib/api'
import { keysOptions } from '../lib/queries'
import { date } from '../lib/format'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, PageHeader, Status } from '../components/ui'
import { KeyForm } from '../components/key-form'
import type { APIKey, Schema } from '../lib/types'

export default function KeysPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { data } = useSuspenseQuery(keysOptions())
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<APIKey | null>(null)
  const [secret, setSecret] = useState('')
  const [copyError, setCopyError] = useState<Error | null>(null)
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['keys'] })
  const create = useMutation({
    mutationFn: (body: Schema['KeyInput']) =>
      api.POST('/api/token/', { body }).then((result) => unwrap(result)),
    onSuccess: (key) => {
      setSecret(key.key)
      setCreating(false)
      void refresh()
    },
  })
  const remove = useMutation({
    mutationFn: (id: APIKey['id']) =>
      api.DELETE('/api/token/{id}', { params: { path: { id: String(id) } } }),
    onSuccess: refresh,
  })
  const update = useMutation({
    mutationFn: (key: APIKey) =>
      api.PUT('/api/token/', {
        body: {
          ...key,
          status: key.status === 'active' ? 'disabled' : 'active',
        },
      }),
    onSuccess: refresh,
  })
  const edit = useMutation({
    mutationFn: (body: Schema['KeyInput']) => api.PUT('/api/token/', { body }),
    onSuccess: () => {
      setEditing(null)
      void refresh()
    },
  })
  const reveal = useMutation({
    mutationFn: (id: APIKey['id']) =>
      api
        .POST('/api/token/{id}/key', { params: { path: { id: String(id) } } })
        .then((result) => unwrap(result)),
    onSuccess: (value) => setSecret(value.key),
  })
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret)
      setCopyError(null)
    } catch {
      setCopyError(new Error('复制失败，请手动复制'))
    }
  }
  return (
    <>
      <PageHeader
        title="API Key"
        action={
          <Button onClick={() => setCreating(!creating)}>
            <Plus size={16} aria-hidden />
            {t('创建')}
          </Button>
        }
      />
      <ErrorMessage
        error={
          remove.error ?? update.error ?? edit.error ?? reveal.error ?? create.error ?? copyError
        }
      />
      {secret && (
        <section className="secret-panel" aria-live="polite">
          <span>{t('新 API Key')}</span>
          <code>{secret}</code>
          <Button variant="quiet" onClick={copy}>
            <Copy size={16} aria-hidden />
            {t('复制')}
          </Button>
          <Button variant="quiet" onClick={() => setSecret('')}>
            {t('关闭')}
          </Button>
        </section>
      )}
      {creating && (
        <KeyForm
          pending={create.isPending}
          onSave={(body) => create.mutate(body)}
          onCancel={() => setCreating(false)}
        />
      )}
      {editing && (
        <KeyForm
          key={editing.id}
          apiKey={editing}
          pending={edit.isPending}
          onSave={(body) => edit.mutate(body)}
          onCancel={() => setEditing(null)}
        />
      )}
      <DataTable
        rows={data ?? []}
        rowKey={(row) => row.id}
        empty="暂无 API Key，创建后即可调用模型"
        columns={[
          { label: '名称', render: (row) => row.name },
          { label: '密钥', render: (row) => <code>{row.key_prefix}…</code> },
          { label: '状态', render: (row) => <Status value={row.status} /> },
          { label: '创建时间', render: (row) => date(row.created_at) },
          { label: '到期时间', render: (row) => date(row.expires_at) },
          {
            label: '操作',
            render: (row) => (
              <div className="row-actions">
                <Button
                  variant="quiet"
                  onClick={() => {
                    setCreating(false)
                    setEditing(row)
                  }}
                >
                  {t('编辑')}
                </Button>
                <Button
                  variant="quiet"
                  disabled={reveal.isPending}
                  onClick={() => reveal.mutate(row.id)}
                >
                  {t('查看密钥')}
                </Button>
                <Button
                  variant="quiet"
                  disabled={update.isPending}
                  onClick={() => update.mutate(row)}
                >
                  {t(row.status === 'active' ? '停用' : '启用')}
                </Button>
                <Button
                  variant="danger"
                  aria-label={`${t('删除')} ${row.name}`}
                  disabled={remove.isPending}
                  onClick={() => {
                    if (window.confirm(t('删除后无法恢复，确认删除？'))) remove.mutate(row.id)
                  }}
                >
                  <Trash2 size={16} aria-hidden />
                </Button>
              </div>
            ),
          },
        ]}
      />
    </>
  )
}
