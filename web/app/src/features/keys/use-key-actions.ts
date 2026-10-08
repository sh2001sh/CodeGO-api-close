import { useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { confirmAction } from '../../components/ui'
import { useTranslation } from '../../lib/i18n'
import type { APIKey, Schema } from '../../lib/types'

function useKeyFilters(keys: APIKey[]) {
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<'' | APIKey['status']>('')
  const filtered = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    return keys.filter((key) => {
      if (statusFilter && key.status !== statusFilter) return false
      if (keyword && !key.name.toLowerCase().includes(keyword)) return false
      return true
    })
  }, [keys, search, statusFilter])
  return { search, setSearch, statusFilter, setStatusFilter, filtered }
}

function useKeySelection(filtered: APIKey[]) {
  const [selected, setSelected] = useState<Set<APIKey['id']>>(new Set())
  const toggle = (id: APIKey['id'], checked: boolean) => {
    setSelected((current) => {
      const next = new Set(current)
      if (checked) next.add(id)
      else next.delete(id)
      return next
    })
  }
  const toggleAll = (checked: boolean) =>
    setSelected(checked ? new Set(filtered.map((key) => key.id)) : new Set())
  return { selected, toggle, toggleAll, clear: () => setSelected(new Set()) }
}

/**
 * Owns search/filter, batch selection, and the key CRUD mutations for the keys page.
 * Batch actions run sequentially against the per-item endpoints (no bulk endpoint
 * exists) and report partial failure instead of silently stopping.
 */
export function useKeyActions(keys: APIKey[]) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const filters = useKeyFilters(keys)
  const selection = useKeySelection(filters.filtered)
  const [secret, setSecret] = useState('')
  const [batchError, setBatchError] = useState<Error | null>(null)
  const [batchPending, setBatchPending] = useState(false)
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['keys'] })

  const create = useMutation({
    mutationFn: (body: Schema['KeyInput']) =>
      api.POST('/api/token/', { body }).then((result) => unwrap(result)),
    onSuccess: (key) => {
      setSecret(key.key)
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
        body: { ...key, status: key.status === 'active' ? 'disabled' : 'active' },
      }),
    onSuccess: refresh,
  })
  const edit = useMutation({
    mutationFn: (body: Schema['KeyInput']) => api.PUT('/api/token/', { body }),
    onSuccess: refresh,
  })
  const reveal = useMutation({
    mutationFn: (id: APIKey['id']) =>
      api
        .POST('/api/token/{id}/key', { params: { path: { id: String(id) } } })
        .then((result) => unwrap(result)),
    onSuccess: (value) => setSecret(value.key),
  })

  const runBatch = async (action: (key: APIKey) => Promise<unknown>) => {
    setBatchPending(true)
    setBatchError(null)
    const targets = filters.filtered.filter((key) => selection.selected.has(key.id))
    let failures = 0
    for (const key of targets) {
      try {
        // Sequential by design: only per-item endpoints exist, so each result is
        // awaited before the next request and partial failure is reported honestly.
        await action(key)
      } catch {
        failures += 1
      }
    }
    setBatchPending(false)
    selection.clear()
    void refresh()
    if (failures > 0)
      setBatchError(
        new Error(
          t('{failed} / {total} 项操作失败，请检查后重试', {
            failed: failures,
            total: targets.length,
          }),
        ),
      )
  }

  const batchSetStatus = (status: 'active' | 'disabled') =>
    runBatch((key) => api.PUT('/api/token/', { body: { ...key, status } }))

  const batchDelete = async () => {
    const ok = await confirmAction({
      title: '批量删除 API Key',
      description: t('将删除 {count} 个 Key，删除后无法恢复，确认继续？', {
        count: selection.selected.size,
      }),
      confirmLabel: '确认删除',
      danger: true,
    })
    if (ok)
      void runBatch((key) =>
        api.DELETE('/api/token/{id}', { params: { path: { id: String(key.id) } } }),
      )
  }

  return {
    filters,
    selection,
    secret,
    setSecret,
    batchError,
    batchPending,
    create,
    remove,
    update,
    edit,
    reveal,
    batchSetStatus,
    batchDelete,
  }
}
