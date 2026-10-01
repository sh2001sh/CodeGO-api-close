import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, PageHeader, Status } from '../components/ui'
import { ChannelForm, type Channel } from '../components/channel-form'
import { CatalogConfiguration } from '../components/catalog-configuration'

export default function ChannelsPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [editing, setEditing] = useState<Channel | 'new' | null>(null)
  const [configuration, setConfiguration] = useState(false)
  const { data } = useSuspenseQuery(
    resourceOptions(
      'channels',
      (signal) =>
        api
          .GET('/api/catalog/channels', {
            signal,
            params: { query: { page, page_size: 30, keyword } },
          })
          .then((result) => unwrap(result)),
      [page, keyword],
    ),
  )
  const save = useMutation({
    mutationFn: (channel: Channel) =>
      channel.id
        ? api.PUT('/api/catalog/channels/{id}', {
            params: { path: { id: String(channel.id) } },
            body: channel,
          })
        : api.POST('/api/catalog/channels', { body: channel }),
    onSuccess: () => {
      setEditing(null)
      void queryClient.invalidateQueries({ queryKey: ['channels'] })
    },
  })
  const remove = useMutation({
    mutationFn: (id: Channel['id']) =>
      api.DELETE('/api/catalog/channels/{id}', { params: { path: { id: String(id) } } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['channels'] }),
  })
  return (
    <>
      <PageHeader
        title="渠道"
        action={
          <>
            <Button variant="quiet" onClick={() => setConfiguration(!configuration)}>
              {t('分组与定价')}
            </Button>
            <Button onClick={() => setEditing('new')}>
              <Plus size={16} aria-hidden />
              {t('创建')}
            </Button>
          </>
        }
      />
      <ErrorMessage error={save.error ?? remove.error} />
      {configuration && <CatalogConfiguration />}
      {editing && (
        <ChannelForm
          key={editing === 'new' ? 'new' : editing.id}
          channel={editing === 'new' ? undefined : editing}
          pending={save.isPending}
          onSave={(channel) => save.mutate(channel)}
          onCancel={() => setEditing(null)}
        />
      )}
      <form
        className="filters"
        onSubmit={(event) => {
          event.preventDefault()
          setKeyword(String(new FormData(event.currentTarget).get('keyword')))
          setPage(1)
        }}
      >
        <label className="field" htmlFor="channel-search">
          <span>{t('名称')}</span>
          <input id="channel-search" name="keyword" />
        </label>
        <Button type="submit">{t('搜索')}</Button>
      </form>
      <DataTable
        rows={data.items ?? []}
        rowKey={(row) => row.id}
        columns={[
          { label: '名称', render: (row) => row.name },
          { label: 'Provider', render: (row) => row.provider },
          { label: '模型', render: (row) => row.models?.join(', ') },
          { label: '分组', render: (row) => row.groups?.join(', ') },
          { label: '优先级', render: (row) => row.priority, numeric: true },
          { label: '状态', render: (row) => <Status value={row.status} /> },
          {
            label: '操作',
            render: (row) => (
              <div className="row-actions">
                <Button variant="quiet" onClick={() => setEditing(row)}>
                  {t('编辑')}
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
      <div className="filters section">
        <Button variant="quiet" disabled={page === 1} onClick={() => setPage(page - 1)}>
          {t('上一页')}
        </Button>
        <span>
          {page} / {Math.max(1, Math.ceil(Number(data.total) / 30))}
        </span>
        <Button
          variant="quiet"
          disabled={page * 30 >= Number(data.total)}
          onClick={() => setPage(page + 1)}
        >
          {t('下一页')}
        </Button>
      </div>
    </>
  )
}
