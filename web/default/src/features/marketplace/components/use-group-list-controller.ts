import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceAutoRoutePoolUpdate,
  useMarketplaceBatchTest,
  useMarketplaceBatchTestQuery,
} from '../hooks'
import {
  appendAutoRoutePoolGroup,
  selectedAutoRoutePoolGroupIDs,
} from '../lib/auto-route-pool'
import type { MarketplaceBatchTestItem, MarketplaceGroup } from '../types'

function collectAvailableModels(
  groups: MarketplaceGroup[],
  selectedGroupIDs: string[]
) {
  const selected = selectedGroupIDs
    .map((id) => groups.find((group) => group.id === id))
    .filter((group): group is MarketplaceGroup => Boolean(group))
  if (selected.length === 0) return []
  const supportedByAll = new Set(
    selected
      .slice(1)
      .flatMap((group) => group.models.map((model) => model.toLowerCase()))
  )
  return selected[0].models.filter(
    (model) => selected.length === 1 || supportedByAll.has(model.toLowerCase())
  )
}

function collectTestResults(items?: MarketplaceBatchTestItem[]) {
  const results: Record<string, 'passed' | 'failed'> = {}
  items?.forEach((item) => {
    if (item.status === 'passed' || item.status === 'failed') {
      results[item.group_id] = item.status
    }
  })
  return results
}

function createPendingItems(
  groups: MarketplaceGroup[],
  selectedGroupIDs: string[],
  results: Record<string, 'passed' | 'failed'>
): MarketplaceBatchTestItem[] {
  return selectedGroupIDs.map((id) => ({
    group_id: id,
    group_name:
      groups.find((item) => item.id === id)?.system_display_name ?? id,
    status: results[id] ?? 'queued',
    latency_ms: 0,
    quota_charged: 0,
    log_created: false,
  }))
}

function useBatchSelection(groups: MarketplaceGroup[]) {
  const [selectedGroupIDs, setSelectedGroupIDs] = useState<string[]>([])
  const [testModel, setTestModel] = useState('')
  const [resultGroupIDs, setResultGroupIDs] = useState<string[]>([])
  const [batchID, setBatchID] = useState('')
  const batchQuery = useMarketplaceBatchTestQuery(batchID)
  const selectableGroups = useMemo(
    () => groups.filter((group) => group.lifecycle_status === 'active'),
    [groups]
  )
  const availableModels = useMemo(
    () => collectAvailableModels(selectableGroups, selectedGroupIDs),
    [selectableGroups, selectedGroupIDs]
  )
  const testResults = useMemo(
    () => collectTestResults(batchQuery.data?.items),
    [batchQuery.data?.items]
  )
  const selectedModel = availableModels.includes(testModel) ? testModel : ''
  const testState = !batchID
    ? ('idle' as const)
    : ['completed', 'failed'].includes(batchQuery.data?.status ?? '')
      ? ('done' as const)
      : ('running' as const)
  const selectedResultGroupIDs = resultGroupIDs.filter(
    (groupID) => testResults[groupID] === 'passed'
  )
  const items =
    batchQuery.data?.items ??
    createPendingItems(groups, selectedGroupIDs, testResults)
  return {
    selectedGroupIDs,
    setSelectedGroupIDs,
    testModel,
    setTestModel,
    resultGroupIDs,
    setResultGroupIDs,
    batchID,
    setBatchID,
    batchQuery,
    selectableGroups,
    availableModels,
    selectedModel,
    testResults,
    testState,
    selectedResultGroupIDs,
    items,
  }
}

type BatchSelection = ReturnType<typeof useBatchSelection>

function useBatchSelectionActions(selection: BatchSelection) {
  const toggleSelected = (groupID: string) => {
    selection.setSelectedGroupIDs((current) =>
      current.includes(groupID)
        ? current.filter((id) => id !== groupID)
        : current.length >= 5
          ? current
          : [...current, groupID]
    )
  }
  const reset = () => {
    selection.setSelectedGroupIDs([])
    selection.setTestModel('')
    selection.setBatchID('')
    selection.setResultGroupIDs([])
  }
  const togglePassed = () => {
    const passed = Object.entries(selection.testResults)
      .filter(([, status]) => status === 'passed')
      .map(([groupID]) => groupID)
    selection.setResultGroupIDs((current) =>
      current.filter((id) => selection.testResults[id] === 'passed').length ===
      passed.length
        ? []
        : passed
    )
  }
  const toggleResult = (groupID: string) => {
    selection.setResultGroupIDs((current) =>
      current.includes(groupID)
        ? current.filter((id) => id !== groupID)
        : [...current, groupID]
    )
  }
  return { toggleSelected, reset, togglePassed, toggleResult }
}

function useRoutePoolState(enabled?: boolean) {
  const [addingGroupID, setAddingGroupID] = useState('')
  const [routeAdding, setRouteAdding] = useState(false)
  const query = useMarketplaceAutoRoutePool(enabled)
  const update = useMarketplaceAutoRoutePoolUpdate()
  const selectedGroups = useMemo(
    () => new Set(selectedAutoRoutePoolGroupIDs(query.data?.items ?? [])),
    [query.data?.items]
  )
  return {
    addingGroupID,
    setAddingGroupID,
    routeAdding,
    setRouteAdding,
    query,
    update,
    selectedGroups,
  }
}

type RoutePoolState = ReturnType<typeof useRoutePoolState>

function useRunBatchTest(selection: BatchSelection) {
  const { t } = useTranslation()
  const mutation = useMarketplaceBatchTest()
  return async () => {
    if (selection.selectedGroupIDs.length === 0) {
      return toast.error(t('请先选择 1-5 个可用分组'))
    }
    const model = selection.selectedModel.trim()
    if (!model) return toast.error(t('请选择一个模型'))
    try {
      const task = await mutation.mutateAsync({
        groupIds: selection.selectedGroupIDs,
        model,
      })
      selection.setBatchID(task.id)
      selection.setResultGroupIDs([])
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('批量测试启动失败')
      )
    }
  }
}

function useAddPassedGroups(
  selection: BatchSelection,
  routePool: RoutePoolState
) {
  const { t } = useTranslation()
  return async () => {
    const passed = selection.selectedResultGroupIDs
    if (passed.length === 0 || routePool.routeAdding) return
    routePool.setRouteAdding(true)
    try {
      const pool =
        routePool.query.data ?? (await routePool.query.refetch()).data
      if (!pool) throw new Error(t('无法读取 Auto 路由池'))
      let next = selectedAutoRoutePoolGroupIDs(pool.items)
      for (const id of passed) {
        const items = pool.items.map((item) => ({
          ...item,
          selected: next.includes(item.group_id),
        }))
        next = appendAutoRoutePoolGroup(items, id)
      }
      await routePool.update.mutateAsync({ groupIds: next })
      toast.success(t('已将测试通过的分组加入路由池'))
      selection.setResultGroupIDs([])
      selection.setBatchID('')
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('加入路由池失败'))
    } finally {
      routePool.setRouteAdding(false)
    }
  }
}

function useAddSingleGroup(routePool: RoutePoolState) {
  const { t } = useTranslation()
  return async (groupID: string) => {
    if (routePool.addingGroupID || routePool.selectedGroups.has(groupID)) return
    routePool.setAddingGroupID(groupID)
    try {
      const pool =
        routePool.query.data ?? (await routePool.query.refetch()).data
      if (!pool) throw new Error(t('无法读取 Auto 路由池'))
      if (
        pool.items.some((item) => item.group_id === groupID && item.selected)
      ) {
        return
      }
      const groupIDs = appendAutoRoutePoolGroup(pool.items, groupID)
      await routePool.update.mutateAsync({ groupIds: groupIDs })
      toast.success(t('已添加到 Auto 路由池'))
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('添加到路由池失败')
      )
    } finally {
      routePool.setAddingGroupID('')
    }
  }
}

/** Coordinates list expansion, batch testing, and Auto route pool actions. */
export function useGroupListController(
  groups: MarketplaceGroup[],
  routePoolEnabled?: boolean
) {
  const [expanded, setExpanded] = useState('')
  const selection = useBatchSelection(groups)
  const selectionActions = useBatchSelectionActions(selection)
  const routePool = useRoutePoolState(routePoolEnabled)
  const runBatchTest = useRunBatchTest(selection)
  const addPassedGroups = useAddPassedGroups(selection, routePool)
  const addSingleGroup = useAddSingleGroup(routePool)
  const toggleExpanded = (groupID: string) =>
    setExpanded((current) => (current === groupID ? '' : groupID))
  return {
    expanded,
    toggleExpanded,
    selection,
    selectionActions,
    routePool,
    runBatchTest,
    addPassedGroups,
    addSingleGroup,
  }
}
