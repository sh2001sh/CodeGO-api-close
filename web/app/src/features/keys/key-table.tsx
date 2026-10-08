import { Boxes, FileCode, Settings2 } from 'lucide-react'
import { date } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { DataTable } from '../../components/data-table'
import { Button, Status, confirmAction } from '../../components/ui'
import type { APIKey } from '../../lib/types'
import type { useKeyActions } from './use-key-actions'

export function KeyTable(props: {
  rows: APIKey[]
  actions: ReturnType<typeof useKeyActions>
  onEdit: (key: APIKey) => void
  onShowModels: (key: APIKey) => void
  onTest: (key: APIKey) => void
  onConfig: (key: APIKey) => void
}) {
  const { t } = useTranslation()
  const { selection, remove, update, reveal } = props.actions
  return (
    <DataTable
      rows={props.rows}
      rowKey={(row) => row.id}
      empty="暂无 API Key，创建后即可调用模型"
      columns={[
        {
          label: '',
          render: (row) => (
            <input
              type="checkbox"
              aria-label={`${t('选择')} ${row.name}`}
              checked={selection.selected.has(row.id)}
              onChange={(event) => selection.toggle(row.id, event.target.checked)}
            />
          ),
        },
        { label: '名称', render: (row) => row.name },
        { label: '密钥', render: (row) => <code>{row.key_prefix}…</code> },
        { label: '状态', render: (row) => <Status value={row.status} /> },
        { label: '创建时间', render: (row) => date(row.created_at) },
        { label: '到期时间', render: (row) => date(row.expires_at) },
        {
          label: '操作',
          render: (row) => (
            <div className="row-actions">
              <Button variant="quiet" onClick={() => props.onEdit(row)}>
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
              <Button variant="quiet" onClick={() => props.onShowModels(row)}>
                <Boxes size={14} aria-hidden />
                {t('可用模型')}
              </Button>
              <Button variant="quiet" onClick={() => props.onTest(row)}>
                <Settings2 size={14} aria-hidden />
                {t('连通性测试')}
              </Button>
              <Button variant="quiet" onClick={() => props.onConfig(row)}>
                <FileCode size={14} aria-hidden />
                {t('客户端配置')}
              </Button>
              <Button
                variant="danger"
                aria-label={`${t('删除')} ${row.name}`}
                disabled={remove.isPending}
                onClick={async () => {
                  const ok = await confirmAction({
                    title: '删除 API Key',
                    description: `${t('删除「')}${row.name}${t('」后无法恢复，确认删除？')}`,
                    confirmLabel: '确认删除',
                    danger: true,
                  })
                  if (ok) remove.mutate(row.id)
                }}
              >
                {t('删除')}
              </Button>
            </div>
          ),
        },
      ]}
    />
  )
}
