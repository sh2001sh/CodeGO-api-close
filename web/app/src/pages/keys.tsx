import { useState } from 'react'
import { useSuspenseQuery } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { Link, useSearch } from '@tanstack/react-router'
import { keysOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage, PageHeader, Drawer } from '../components/ui'
import { KeyForm } from '../components/key-form'
import { useKeyActions } from '../features/keys/use-key-actions'
import { KeyFilterBar, KeyBatchBar } from '../features/keys/filter-bar'
import { KeyTable } from '../features/keys/key-table'
import { AvailableModelsDialog } from '../features/keys/available-models-dialog'
import { ConnectivityTestDialog } from '../features/keys/connectivity-test'
import { ClientConfigDialog } from '../features/keys/client-config-dialog'
import type { APIKey } from '../lib/types'

export default function KeysPage() {
  const { t } = useTranslation()
  const requested = useSearch({ from: '/_authenticated/keys' })
  const keys = useSuspenseQuery(keysOptions()).data ?? []
  const actions = useKeyActions(keys)
  const [creating, setCreating] = useState(false)
  const [editing, setEditing] = useState<APIKey | null>(null)
  const [modelsFor, setModelsFor] = useState<APIKey | null>(null)
  const [testingFor, setTestingFor] = useState<APIKey | null>(null)
  const [configFor, setConfigFor] = useState<APIKey | null>(null)
  const [copyError, setCopyError] = useState<Error | null>(null)
  const { filters, selection, create, remove, update, edit, reveal } = actions

  const copySecret = async () => {
    try {
      await navigator.clipboard.writeText(actions.secret)
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
          <Button onClick={() => setCreating(true)}>
            <Plus size={16} aria-hidden />
            {t('创建')}
          </Button>
        }
      />
      {requested.group && (
        <p className="notice">
          {t('创建 Key 后返回所选分组完成绑定。')}{' '}
          <Link to="/channel-market" search={{ group: requested.group }}>
            {t('返回所选分组')}
          </Link>
        </p>
      )}
      <ErrorMessage
        error={
          remove.error ?? update.error ?? edit.error ?? reveal.error ?? create.error ?? copyError
        }
      />
      {actions.secret && (
        <section className="secret-panel" aria-live="polite">
          <span>{t('新 API Key')}</span>
          <code>{actions.secret}</code>
          <Button variant="quiet" onClick={copySecret}>
            {t('复制')}
          </Button>
          <Button variant="quiet" onClick={() => actions.setSecret('')}>
            {t('关闭')}
          </Button>
        </section>
      )}

      <KeyFilterBar
        search={filters.search}
        onSearchChange={filters.setSearch}
        statusFilter={filters.statusFilter}
        onStatusChange={filters.setStatusFilter}
      />
      <KeyBatchBar
        total={filters.filtered.length}
        selectedCount={selection.selected.size}
        allSelected={selection.selected.size === filters.filtered.length}
        pending={actions.batchPending}
        onToggleAll={selection.toggleAll}
        onEnable={() => actions.batchSetStatus('active')}
        onDisable={() => actions.batchSetStatus('disabled')}
        onDelete={() => void actions.batchDelete()}
      />
      <ErrorMessage error={actions.batchError} />

      {creating && (
        <Drawer open onOpenChange={(open) => !open && setCreating(false)} title="创建 API Key">
          <KeyForm
            pending={create.isPending}
            onSave={(body) => create.mutate(body, { onSuccess: () => setCreating(false) })}
            onCancel={() => setCreating(false)}
          />
        </Drawer>
      )}
      {editing && (
        <Drawer open onOpenChange={(open) => !open && setEditing(null)} title="编辑 API Key">
          <KeyForm
            key={editing.id}
            apiKey={editing}
            pending={edit.isPending}
            onSave={(body) => edit.mutate(body, { onSuccess: () => setEditing(null) })}
            onCancel={() => setEditing(null)}
          />
        </Drawer>
      )}

      <KeyTable
        rows={filters.filtered}
        actions={actions}
        onEdit={setEditing}
        onShowModels={setModelsFor}
        onTest={setTestingFor}
        onConfig={setConfigFor}
      />

      <AvailableModelsDialog apiKey={modelsFor} onClose={() => setModelsFor(null)} />
      <ConnectivityTestDialog apiKey={testingFor} onClose={() => setTestingFor(null)} />
      <ClientConfigDialog apiKey={configFor} onClose={() => setConfigFor(null)} />
    </>
  )
}
