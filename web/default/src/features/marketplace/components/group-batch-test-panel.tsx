import {
  CheckCircle2,
  Clock3,
  LoaderCircle,
  ListFilter,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { formatDuration } from '../lib/format'
import type { MarketplaceBatchTestItem } from '../types'

type TestState = 'idle' | 'running' | 'done'

type GroupBatchTestPanelProps = {
  selectedGroupIDs: string[]
  availableModels: string[]
  selectedModel: string
  testState: TestState
  testResults: Record<string, 'passed' | 'failed'>
  selectedResultGroupIDs: string[]
  routeAdding: boolean
  batchModel?: string
  items: MarketplaceBatchTestItem[]
  onModelChange: (model: string) => void
  onRun: () => void
  onTogglePassed: () => void
  onAddPassed: () => void
  onReset: () => void
  onToggleResult: (groupID: string) => void
}

/** Renders the user-funded connectivity test controls and their results. */
export function GroupBatchTestPanel(props: GroupBatchTestPanelProps) {
  return (
    <>
      <BatchTestControls {...props} />
      {props.testState !== 'idle' && <BatchTestResults {...props} />}
    </>
  )
}

function BatchTestControls(props: GroupBatchTestPanelProps) {
  const { t } = useTranslation()
  const hasSelection = props.selectedGroupIDs.length > 0

  return (
    <div className='border-border bg-card rounded-md border px-3 py-3 sm:px-4'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div className='flex items-center gap-2 text-sm font-semibold'>
          <ListFilter className='text-primary size-4' aria-hidden='true' />
          {t('分组连通性测试')}
        </div>
        <span className='text-muted-foreground text-xs tabular-nums'>
          {hasSelection
            ? t('已选 {{count}} 个分组', {
                count: props.selectedGroupIDs.length,
              })
            : t('请选择测试分组')}
        </span>
      </div>
      <BatchTestModelControls {...props} />
      {hasSelection && props.availableModels.length === 0 && (
        <p className='text-destructive mt-2 text-xs'>
          {t('当前选择的分组没有共同模型，请减少分组或重新选择。')}
        </p>
      )}
      <BatchTestActions {...props} />
    </div>
  )
}

function BatchTestActions(props: GroupBatchTestPanelProps) {
  const { t } = useTranslation()
  const hasSelection = props.selectedGroupIDs.length > 0
  const hasPassed = Object.values(props.testResults).some(
    (status) => status === 'passed'
  )
  return (
    <div className='mt-2 flex justify-end'>
      {props.testState === 'done' && (
        <>
          <Button
            size='sm'
            variant='ghost'
            disabled={props.routeAdding || !hasPassed}
            onClick={props.onTogglePassed}
          >
            {props.selectedResultGroupIDs.length > 0
              ? t('取消选择通过项')
              : t('全选通过项')}
          </Button>
          <Button
            size='sm'
            variant='outline'
            disabled={
              props.routeAdding || props.selectedResultGroupIDs.length === 0
            }
            onClick={props.onAddPassed}
          >
            {t('加入路由池')} ({props.selectedResultGroupIDs.length})
          </Button>
        </>
      )}
      <Button
        variant='ghost'
        size='sm'
        disabled={!hasSelection && props.testState === 'idle'}
        onClick={props.onReset}
      >
        {t('清空')}
      </Button>
    </div>
  )
}

function BatchTestModelControls(props: GroupBatchTestPanelProps) {
  const { t } = useTranslation()
  const hasSelection = props.selectedGroupIDs.length > 0
  const placeholder = !hasSelection
    ? t('先选择分组')
    : props.availableModels.length === 0
      ? t('所选分组没有共同模型')
      : t('搜索并选择模型')

  return (
    <div className='mt-3 grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(14rem,22rem)_auto] sm:items-end'>
      <div className='text-muted-foreground flex min-h-9 items-center rounded-md border border-dashed px-3 text-xs'>
        {hasSelection
          ? t('将测试已勾选的 {{count}} 个分组', {
              count: props.selectedGroupIDs.length,
            })
          : t('在下方列表勾选至少一个可用分组')}
      </div>
      <label className='min-w-0'>
        <span className='text-muted-foreground mb-1 block text-xs'>
          {t('测试模型')}
        </span>
        <Combobox
          options={props.availableModels.map((model) => ({
            value: model,
            label: model,
          }))}
          value={props.selectedModel}
          onValueChange={(value) => props.onModelChange(value ?? '')}
          placeholder={placeholder}
          searchPlaceholder={t('搜索模型')}
          emptyText={t('没有匹配的共同模型')}
          className='w-full'
        />
      </label>
      <Button
        size='sm'
        disabled={
          !hasSelection || !props.selectedModel || props.testState === 'running'
        }
        onClick={props.onRun}
      >
        {props.testState === 'running' ? t('测试中…') : t('开始测试')}
      </Button>
    </div>
  )
}

function BatchTestResults(props: GroupBatchTestPanelProps) {
  const { t } = useTranslation()
  return (
    <div className='border-border bg-primary/[0.04] rounded-md border px-3 py-3 text-xs'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='font-medium'>
          {props.testState === 'running'
            ? t('正在按分组执行测试')
            : t('批量测试完成')}
        </div>
        {props.batchModel && (
          <span className='text-foreground font-mono'>
            {t('模型 {{model}}', { model: props.batchModel })}
          </span>
        )}
      </div>
      <div className='divide-border/60 mt-2 divide-y'>
        {props.items.map((item) => (
          <BatchTestResultRow
            key={item.group_id}
            item={item}
            selected={props.selectedResultGroupIDs.includes(item.group_id)}
            onToggle={() => props.onToggleResult(item.group_id)}
          />
        ))}
      </div>
    </div>
  )
}

function BatchTestResultRow(props: {
  item: MarketplaceBatchTestItem
  selected: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const item = props.item
  const status = batchStatusPresentation(item.status, t)
  const StatusIcon = status.icon
  return (
    <div className='flex flex-wrap items-center gap-x-3 gap-y-1 py-2 first:pt-0 last:pb-0'>
      {item.status === 'passed' && (
        <input
          type='checkbox'
          checked={props.selected}
          onChange={props.onToggle}
          aria-label={t('选择 {{name}} 加入路由池', {
            name: item.group_name || item.group_id,
          })}
          className='accent-primary size-4 shrink-0'
        />
      )}
      <span className='flex min-w-40 items-center gap-1.5 font-medium'>
        <StatusIcon className={status.className} aria-hidden='true' />
        {item.group_name || item.group_id}
      </span>
      <span className={status.className}>{status.label}</span>
      <span className='text-muted-foreground inline-flex items-center gap-1 tabular-nums'>
        <Clock3 className='size-3' aria-hidden='true' />
        {item.latency_ms > 0 ? formatDuration(item.latency_ms) : t('等待中')}
      </span>
      <BatchTestResultMetadata item={item} />
      {item.error && (
        <span className='text-destructive basis-full break-words sm:basis-auto'>
          {item.error}
        </span>
      )}
    </div>
  )
}

function BatchTestResultMetadata(props: { item: MarketplaceBatchTestItem }) {
  const { t } = useTranslation()
  const item = props.item
  return (
    <>
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
    </>
  )
}

function batchStatusPresentation(
  status: MarketplaceBatchTestItem['status'],
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
