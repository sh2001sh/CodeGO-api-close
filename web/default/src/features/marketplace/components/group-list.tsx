import { useEffect, useMemo, useState } from 'react'
import { ShieldCheck, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { StaggerContainer, StaggerItem } from '@/components/page-transition'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceBatchTest,
  useMarketplaceBatchTestQuery,
  useMarketplaceAutoRoutePoolUpdate,
} from '../hooks'
import {
  appendAutoRoutePoolGroup,
  selectedAutoRoutePoolGroupIDs,
} from '../lib/auto-route-pool'
import type { MarketplaceGroup } from '../types'
import { GroupMarketItem } from './group-rows'

export function MarketplaceGroupList(props: {
  groups: MarketplaceGroup[]
  loading: boolean
  error: boolean
  routePoolEnabled?: boolean
  onRetry: () => void
  model?: string
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState('')
  const [addingGroupID, setAddingGroupID] = useState('')
  const [selectedGroupIDs, setSelectedGroupIDs] = useState<string[]>([])
  const [testState, setTestState] = useState<'idle' | 'running' | 'done'>(
    'idle'
  )
  const [testResults, setTestResults] = useState<
    Record<string, 'passed' | 'failed'>
  >({})
  const [routeAdding, setRouteAdding] = useState(false)
  const batchStart = useMarketplaceBatchTest()
  const [batchID, setBatchID] = useState('')
  const batchQuery = useMarketplaceBatchTestQuery(batchID)
  const autoPool = useMarketplaceAutoRoutePool(props.routePoolEnabled)
  const autoPoolUpdate = useMarketplaceAutoRoutePoolUpdate()
  const routePoolGroupIDs = useMemo(
    () => selectedAutoRoutePoolGroupIDs(autoPool.data?.items ?? []),
    [autoPool.data?.items]
  )
  const selectedGroups = useMemo(
    () => new Set(routePoolGroupIDs),
    [routePoolGroupIDs]
  )
  const selectableGroups = props.groups.filter(
    (group) => group.lifecycle_status === 'active'
  )
  const toggleSelected = (groupID: string) => {
    setSelectedGroupIDs((current) =>
      current.includes(groupID)
        ? current.filter((id) => id !== groupID)
        : current.length >= 5
          ? current
          : [...current, groupID]
    )
  }

  const runBatchTest = async () => {
    const model =
      props.model ||
      props.groups.find((group) => selectedGroupIDs.includes(group.id))
        ?.models[0]
    if (!model) return toast.error(t('请选择一个模型'))
    try {
      const task = await batchStart.mutateAsync({
        groupIds: selectedGroupIDs,
        model,
      })
      setBatchID(task.id)
      setTestState('running')
      setTestResults({})
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('批量测试启动失败')
      )
    }
  }

  useEffect(() => {
    if (!batchQuery.data) return
    const next: Record<string, 'passed' | 'failed'> = {}
    batchQuery.data.items.forEach((item) => {
      if (item.status === 'passed' || item.status === 'failed')
        next[item.group_id] = item.status
    })
    setTestResults(next)
    if (
      batchQuery.data.status === 'completed' ||
      batchQuery.data.status === 'failed'
    )
      setTestState('done')
  }, [batchQuery.data])

  const addPassedToRoutePool = async () => {
    const passed = selectedGroupIDs.filter((id) => testResults[id] === 'passed')
    if (passed.length === 0 || routeAdding) return
    setRouteAdding(true)
    try {
      const pool = autoPool.data ?? (await autoPool.refetch()).data
      if (!pool) throw new Error(t('无法读取 Auto 路由池'))
      let next = selectedAutoRoutePoolGroupIDs(pool.items)
      for (const id of passed)
        next = appendAutoRoutePoolGroup(
          pool.items.map((item) => ({
            ...item,
            selected: next.includes(item.group_id),
          })),
          id
        )
      await autoPoolUpdate.mutateAsync({ groupIds: next })
      toast.success(t('已将测试通过的分组加入路由池'))
      setSelectedGroupIDs([])
      setTestState('idle')
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('加入路由池失败'))
    } finally {
      setRouteAdding(false)
    }
  }

  if (props.loading) return <GroupListSkeleton />
  if (props.error) return <GroupListError onRetry={props.onRetry} />
  if (props.groups.length === 0) return <GroupListEmpty />

  const toggle = (groupID: string) =>
    setExpanded((current) => (current === groupID ? '' : groupID))
  const addToRoutePool = async (groupID: string) => {
    if (addingGroupID || selectedGroups.has(groupID)) return
    setAddingGroupID(groupID)
    try {
      const pool = autoPool.data ?? (await autoPool.refetch()).data
      if (!pool) throw new Error(t('无法读取 Auto 路由池'))
      if (
        pool.items.some((item) => item.group_id === groupID && item.selected)
      ) {
        return
      }
      const nextGroupIDs = appendAutoRoutePoolGroup(pool.items, groupID)
      await autoPoolUpdate.mutateAsync({ groupIds: nextGroupIDs })
      toast.success(t('已添加到 Auto 路由池'))
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('添加到路由池失败')
      )
    } finally {
      setAddingGroupID('')
    }
  }

  return (
    <div className='bg-muted/25 space-y-1.5 p-2'>
      <div className='border-border bg-card flex flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2'>
        <div className='text-muted-foreground text-xs'>
          <span className='text-foreground font-medium'>{t('批量测试')}</span>
          <span className='ml-2'>
            {t('选择多个可用分组后统一测试，单次最多 5 个。')}
          </span>
        </div>
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-xs tabular-nums'>
            {t('已选 {{count}} 个', { count: selectedGroupIDs.length })}
          </span>
          <Button
            size='sm'
            disabled={selectedGroupIDs.length === 0 || testState === 'running'}
            onClick={() => void runBatchTest()}
          >
            {testState === 'running' ? t('测试中…') : t('开始批量测试')}
          </Button>
          {testState === 'done' && (
            <Button
              size='sm'
              variant='outline'
              disabled={routeAdding}
              onClick={() => void addPassedToRoutePool()}
            >
              {t('通过项加入路由池')} (
              {
                Object.values(testResults).filter((value) => value === 'passed')
                  .length
              }
              )
            </Button>
          )}
          <Button
            variant='ghost'
            size='sm'
            disabled={selectedGroupIDs.length === 0}
            onClick={() => setSelectedGroupIDs([])}
          >
            {t('清空')}
          </Button>
        </div>
      </div>
      {testState !== 'idle' && (
        <div className='border-border bg-primary/[0.04] rounded-md border px-3 py-2 text-xs'>
          <div className='font-medium'>
            {testState === 'running'
              ? t('正在按分组执行测试')
              : t('批量测试完成')}
          </div>
          <div className='text-muted-foreground mt-1 flex flex-wrap gap-x-3 gap-y-1'>
            {selectedGroupIDs.map((id) => {
              const group = props.groups.find((item) => item.id === id)
              const result = testResults[id]
              return (
                <span key={id}>
                  {group?.system_display_name ?? id}：
                  {result === 'passed'
                    ? t('通过')
                    : result === 'failed'
                      ? t('失败')
                      : t('等待')}
                </span>
              )
            })}
          </div>
        </div>
      )}
      <StaggerContainer className='space-y-1.5'>
        {props.groups.map((group) => (
          <StaggerItem key={group.id}>
            <GroupMarketItem
              group={group}
              open={expanded === group.id}
              onToggle={() => toggle(group.id)}
              routePoolSelected={selectedGroups.has(group.id)}
              routePoolBusy={Boolean(addingGroupID) || autoPool.isLoading}
              routePoolAdding={addingGroupID === group.id}
              onAddToRoutePool={() => void addToRoutePool(group.id)}
              showRoutePoolAction={props.routePoolEnabled !== false}
              selectable={selectableGroups.some((item) => item.id === group.id)}
              selected={selectedGroupIDs.includes(group.id)}
              onSelect={() => toggleSelected(group.id)}
            />
          </StaggerItem>
        ))}
      </StaggerContainer>
    </div>
  )
}

function GroupListError(props: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className='flex min-h-64 flex-col items-center justify-center gap-3 px-4 text-center'>
      <div className='bg-destructive/10 text-destructive flex size-11 items-center justify-center rounded-lg'>
        <ShieldCheck className='size-5' />
      </div>
      <div className='font-medium'>{t('分组市场暂时不可用')}</div>
      <p className='text-muted-foreground max-w-md text-sm leading-6'>
        {t('无法加载市场数据，请稍后重试。')}
      </p>
      <Button variant='outline' size='sm' onClick={props.onRetry}>
        {t('重新获取')}
      </Button>
    </div>
  )
}

function GroupListEmpty() {
  const { t } = useTranslation()
  return (
    <div className='flex min-h-64 flex-col items-center justify-center px-4 text-center'>
      <div className='border-primary/30 text-primary bg-primary/[0.04] flex size-12 items-center justify-center rounded-xl border'>
        <Sparkles className='size-5' />
      </div>
      <div className='mt-4 font-medium'>{t('等待首批公开渠道')}</div>
      <p className='text-muted-foreground mt-1 max-w-lg text-sm leading-6 text-pretty'>
        {t(
          '渠道完成检测与管理员审核后会出现在这里；你也可以在“我的渠道”提交自己的模型通道。'
        )}
      </p>
    </div>
  )
}

function GroupListSkeleton() {
  return (
    <div className='space-y-2 p-4'>
      {Array.from({ length: 6 }).map((_, index) => (
        <Skeleton key={index} className='h-20 w-full rounded-md' />
      ))}
    </div>
  )
}
