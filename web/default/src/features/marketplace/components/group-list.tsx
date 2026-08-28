import { useEffect, useMemo, useState } from 'react'
import {
  CheckCircle2,
  Clock3,
  LoaderCircle,
  ShieldCheck,
  Sparkles,
  XCircle,
} from 'lucide-react'
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
import { formatDuration } from '../lib/format'
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
  const [resultSelectedGroupIDs, setResultSelectedGroupIDs] = useState<
    string[]
  >([])
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
    const targetGroupIDs =
      selectedGroupIDs.length > 0
        ? selectedGroupIDs
        : selectableGroups.slice(0, 5).map((group) => group.id)
    const targetGroups = targetGroupIDs
      .map((id) => props.groups.find((group) => group.id === id))
      .filter((group): group is MarketplaceGroup => Boolean(group))
    if (targetGroupIDs.length === 0)
      return toast.error(t('当前没有可测试的可用分组'))
    const model = props.model?.trim() || findSharedModel(targetGroups)
    if (!model) return toast.error(t('请选择一个模型'))
    if (selectedGroupIDs.length === 0 && selectableGroups.length > 5)
      toast.info(t('未选择分组，默认测试当前页前 5 个可用分组'))
    try {
      const task = await batchStart.mutateAsync({
        groupIds: targetGroupIDs,
        model,
      })
      setBatchID(task.id)
      setTestState('running')
      setTestResults({})
      setResultSelectedGroupIDs([])
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
    setResultSelectedGroupIDs((current) =>
      current.filter((groupID) => next[groupID] === 'passed')
    )
    if (
      batchQuery.data.status === 'completed' ||
      batchQuery.data.status === 'failed'
    )
      setTestState('done')
  }, [batchQuery.data])

  const addPassedToRoutePool = async () => {
    const passed = resultSelectedGroupIDs.filter(
      (id) => testResults[id] === 'passed'
    )
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
      setResultSelectedGroupIDs([])
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
            {t(
              '可先直接测试当前页分组，再从结果中选择分组加入路由池；单次最多 5 个。'
            )}
          </span>
        </div>
        <div className='flex items-center gap-2'>
          <span className='text-muted-foreground text-xs tabular-nums'>
            {selectedGroupIDs.length > 0
              ? t('已选 {{count}} 个测试对象', {
                  count: selectedGroupIDs.length,
                })
              : t('未选择时默认测试前 5 个')}
          </span>
          <Button
            size='sm'
            disabled={
              (selectedGroupIDs.length === 0 &&
                selectableGroups.length === 0) ||
              testState === 'running'
            }
            onClick={() => void runBatchTest()}
          >
            {testState === 'running'
              ? t('测试中…')
              : selectedGroupIDs.length > 0
                ? t('开始批量测试')
                : t('测试当前页分组')}
          </Button>
          {testState === 'done' && (
            <>
              <Button
                size='sm'
                variant='ghost'
                disabled={
                  routeAdding ||
                  !Object.values(testResults).some(
                    (status) => status === 'passed'
                  )
                }
                onClick={() => {
                  const passed = Object.entries(testResults)
                    .filter(([, status]) => status === 'passed')
                    .map(([groupID]) => groupID)
                  setResultSelectedGroupIDs((current) =>
                    current.length === passed.length ? [] : passed
                  )
                }}
              >
                {resultSelectedGroupIDs.length > 0
                  ? t('取消选择通过项')
                  : t('全选通过项')}
              </Button>
              <Button
                size='sm'
                variant='outline'
                disabled={routeAdding || resultSelectedGroupIDs.length === 0}
                onClick={() => void addPassedToRoutePool()}
              >
                {t('加入路由池')} ({resultSelectedGroupIDs.length})
              </Button>
            </>
          )}
          <Button
            variant='ghost'
            size='sm'
            disabled={selectedGroupIDs.length === 0 && testState === 'idle'}
            onClick={() => setSelectedGroupIDs([])}
          >
            {t('清空')}
          </Button>
        </div>
      </div>
      {testState !== 'idle' && (
        <div className='border-border bg-primary/[0.04] rounded-md border px-3 py-3 text-xs'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='font-medium'>
              {testState === 'running'
                ? t('正在按分组执行测试')
                : t('批量测试完成')}
            </div>
            <span className='text-muted-foreground'>
              {t(
                '本次测试会按当前用户实际计费规则扣除额度，并写入普通用量日志'
              )}
            </span>
          </div>
          <div className='divide-border/60 mt-2 divide-y'>
            {(
              batchQuery.data?.items ??
              selectedGroupIDs.map((id) => ({
                group_id: id,
                group_name:
                  props.groups.find((item) => item.id === id)
                    ?.system_display_name ?? id,
                status: testResults[id] ?? 'queued',
                latency_ms: 0,
                quota_charged: 0,
                log_created: false,
              }))
            ).map((item) => {
              const status = batchStatusPresentation(item.status, t)
              const StatusIcon = status.icon
              return (
                <div
                  key={item.group_id}
                  className='flex flex-wrap items-center gap-x-3 gap-y-1 py-2 first:pt-0 last:pb-0'
                >
                  {item.status === 'passed' && (
                    <input
                      type='checkbox'
                      checked={resultSelectedGroupIDs.includes(item.group_id)}
                      onChange={() =>
                        setResultSelectedGroupIDs((current) =>
                          current.includes(item.group_id)
                            ? current.filter((id) => id !== item.group_id)
                            : [...current, item.group_id]
                        )
                      }
                      aria-label={t('选择 {{name}} 加入路由池', {
                        name: item.group_name || item.group_id,
                      })}
                      className='accent-primary size-4 shrink-0'
                    />
                  )}
                  <span className='flex min-w-40 items-center gap-1.5 font-medium'>
                    <StatusIcon
                      className={status.className}
                      aria-hidden='true'
                    />
                    {item.group_name || item.group_id}
                  </span>
                  <span className={status.className}>{status.label}</span>
                  <span className='text-muted-foreground inline-flex items-center gap-1 tabular-nums'>
                    <Clock3 className='size-3' aria-hidden='true' />
                    {item.latency_ms > 0
                      ? formatDuration(item.latency_ms)
                      : t('等待中')}
                  </span>
                  {item.started_at && (
                    <span className='text-muted-foreground'>
                      {t('开始 {{time}}', {
                        time: new Date(item.started_at).toLocaleString(),
                      })}
                    </span>
                  )}
                  {item.ended_at && (
                    <span className='text-muted-foreground'>
                      {t('完成 {{time}}', {
                        time: new Date(item.ended_at).toLocaleString(),
                      })}
                    </span>
                  )}
                  {item.status === 'passed' && (
                    <span className='text-muted-foreground'>
                      {t('扣除 {{quota}}', { quota: item.quota_charged ?? 0 })}
                    </span>
                  )}
                  {item.billing_source && (
                    <span className='text-muted-foreground'>
                      {t('来源 {{source}}', { source: item.billing_source })}
                    </span>
                  )}
                  {item.request_id && (
                    <span className='text-muted-foreground font-mono'>
                      {t('请求 {{id}}', { id: item.request_id })}
                    </span>
                  )}
                  {item.error && (
                    <span className='text-destructive basis-full break-words sm:basis-auto'>
                      {item.error}
                    </span>
                  )}
                </div>
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

function batchStatusPresentation(
  status: 'queued' | 'running' | 'passed' | 'failed',
  t: (key: string) => string
) {
  if (status === 'passed') {
    return {
      icon: CheckCircle2,
      className: 'text-emerald-600 dark:text-emerald-400',
      label: t('通过'),
    }
  }
  if (status === 'failed') {
    return {
      icon: XCircle,
      className: 'text-destructive',
      label: t('失败'),
    }
  }
  if (status === 'running') {
    return {
      icon: LoaderCircle,
      className: 'text-primary animate-spin',
      label: t('测试中'),
    }
  }
  return {
    icon: Clock3,
    className: 'text-muted-foreground',
    label: t('等待'),
  }
}

function findSharedModel(groups: MarketplaceGroup[]): string {
  const first = groups[0]?.models ?? []
  if (groups.length <= 1) return first[0] ?? ''
  const supportedByAll = new Set(
    groups
      .slice(1)
      .flatMap((group) => group.models.map((model) => model.toLowerCase()))
  )
  return first.find((model) => supportedByAll.has(model.toLowerCase())) ?? ''
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
