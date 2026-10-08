import { useState } from 'react'
import { useMutation, useSuspenseQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { DataTable } from '../components/data-table'
import { Button, confirmAction, Drawer, ErrorMessage, PageHeader, Status } from '../components/ui'
import { ChannelForm, type Channel } from '../components/channel-form'
import { CatalogConfiguration } from '../components/catalog-configuration'
import {
  ChannelFilterBar,
  emptyChannelFilters,
  matchesChannelFilters,
  type ChannelFilterState,
} from '../features/catalog/channel-filters'
import { ChannelBatchBar } from '../features/catalog/channel-batch'
import {
  ChannelTestCell,
  useChannelTest,
  useChannelTestAll,
} from '../features/catalog/channel-test'
import type { ProbeResult } from '../features/catalog/types'

export default function ChannelsPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [filters, setFilters] = useState<ChannelFilterState>(emptyChannelFilters)
  const [editing, setEditing] = useState<Channel | 'new' | null>(null)
  const [configuration, setConfiguration] = useState(false)
  const [selected, setSelected] = useState<Channel[]>([])
  const [results, setResults] = useState<Record<string, ProbeResult>>({})
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
  const rows = (data.items ?? []).filter((row) => matchesChannelFilters(row, filters))
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
  const test = useChannelTest()
  const testAll = useChannelTestAll()
  const runTest = (row: Channel) =>
    test.mutate(row.id, {
      onSuccess: (result) => setResults((prev) => ({ ...prev, [String(row.id)]: result })),
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
            <Button
              variant="quiet"
              disabled={testAll.isPending}
              onClick={() =>
                testAll.mutate(undefined, {
                  onSuccess: (batch) => {
                    const next: Record<string, ProbeResult> = {}
                    for (const result of batch.data)
                      if (result.id !== undefined) next[String(result.id)] = result
                    setResults(next)
                  },
                })
              }
            >
              {t('测试全部')}
            </Button>
            <Button onClick={() => setEditing('new')}>
              <Plus size={16} aria-hidden />
              {t('创建')}
            </Button>
          </>
        }
      />
      <ErrorMessage error={save.error ?? remove.error ?? test.error ?? testAll.error} />
      {configuration && <CatalogConfiguration />}
      <Drawer
        open={editing !== null}
        onOpenChange={(open) => !open && setEditing(null)}
        title={editing === 'new' ? '创建渠道' : '编辑渠道'}
      >
        {editing && (
          <ChannelForm
            key={editing === 'new' ? 'new' : editing.id}
            channel={editing === 'new' ? undefined : editing}
            pending={save.isPending}
            onSave={(channel) => save.mutate(channel)}
            onCancel={() => setEditing(null)}
          />
        )}
      </Drawer>
      <ChannelFilterBar
        initialKeyword={keyword}
        filters={filters}
        onSearch={(value) => {
          setKeyword(value)
          setPage(1)
        }}
        onFiltersChange={setFilters}
      />
      <ChannelBatchBar selected={selected} onClear={() => setSelected([])} />
      <DataTable
        rows={rows}
        rowKey={(row) => row.id}
        columns={[
          {
            label: '',
            render: (row) => (
              <input
                type="checkbox"
                aria-label={`${t('选择')} ${row.name}`}
                checked={selected.some((item) => item.id === row.id)}
                onChange={(event) =>
                  setSelected(
                    event.target.checked
                      ? [...selected, row]
                      : selected.filter((item) => item.id !== row.id),
                  )
                }
              />
            ),
          },
          { label: '名称', render: (row) => row.name },
          { label: 'Provider', render: (row) => row.provider },
          { label: '模型', render: (row) => row.models?.join(', ') },
          { label: '分组', render: (row) => row.groups?.join(', ') },
          { label: '优先级', render: (row) => row.priority, numeric: true },
          { label: '状态', render: (row) => <Status value={row.status} /> },
          {
            label: '测速',
            render: (row) => (
              <ChannelTestCell
                result={results[String(row.id)]}
                pending={test.isPending}
                onTest={() => runTest(row)}
              />
            ),
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
                  aria-label={`${t('删除')} ${row.name}`}
                  disabled={remove.isPending}
                  onClick={async () => {
                    if (
                      await confirmAction({
                        title: '删除渠道',
                        description: '删除后无法恢复，确认删除？',
                        danger: true,
                      })
                    )
                      remove.mutate(row.id)
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
