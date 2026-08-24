import {
  AlertTriangle,
  CheckCircle2,
  CircleDashed,
  Loader2,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { hasGPT56Model } from '../lib/verification'
import type {
  GPT56MappingStatus,
  MarketplaceGroup,
  ModelConsistencyStatus,
} from '../types'
import { GPT56MappingStatusView } from './gpt56-mapping-report'
import {
  ModelConnectivityResults,
  ModelConsistencyStatusView,
} from './model-verification'

type ModelResultStatus =
  | GPT56MappingStatus
  | ModelConsistencyStatus
  | 'connectivity_passed'
  | 'connectivity_failed'

export function GroupModelResults(props: { group: MarketplaceGroup }) {
  return (
    <div className='mt-2.5 flex flex-wrap gap-1.5'>
      {props.group.models.map((model) => (
        <ModelResult key={model} group={props.group} model={model} />
      ))}
    </div>
  )
}

export function GroupModelVerificationReport(props: {
  group: MarketplaceGroup
}) {
  const { t } = useTranslation()
  const group = props.group
  const hasMappingReport = hasGPT56Model(group.models)
  const checkedAt =
    group.gpt56_mapping_checked_at ??
    group.connectivity_test_checked_at ??
    group.verification_completed_at
  return (
    <section className='border-border bg-muted/10 border-t px-4 py-4 sm:px-5'>
      <div className='flex flex-wrap items-start justify-between gap-3'>
        <div>
          <h5 className='text-sm font-semibold'>{t('模型一致性检测报告')}</h5>
          <p className='text-muted-foreground mt-1 text-xs leading-5'>
            {t('按模型查看检测结论、返回标识、请求耗时和证据明细。')}
          </p>
        </div>
        {(!hasMappingReport || group.model_consistency_status) && (
          <ModelConsistencyStatusView
            status={group.model_consistency_status}
            checkedAt={checkedAt}
          />
        )}
      </div>
      {hasMappingReport && (
        <GPT56MappingStatusView
          models={group.models}
          status={group.gpt56_mapping_status}
          results={group.gpt56_mapping_results ?? []}
          checkedAt={group.gpt56_mapping_checked_at}
          level={group.gpt56_mapping_level}
          trigger={group.gpt56_mapping_trigger}
          embedded
        />
      )}
      {group.model_verification_results.length > 0 && (
        <ModelConnectivityResults
          results={group.model_verification_results}
          embedded={hasMappingReport}
        />
      )}
      {!hasMappingReport && group.model_verification_results.length === 0 && (
        <p className='text-muted-foreground mt-4 text-xs leading-5'>
          {t('当前还没有可展示的逐模型检测记录。')}
        </p>
      )}
    </section>
  )
}

function ModelResult(props: { group: MarketplaceGroup; model: string }) {
  const { t } = useTranslation()
  const status = modelResultStatus(props.group, props.model)
  const passed =
    status === 'matched' ||
    status === 'passed' ||
    status === 'connectivity_passed'
  const failed =
    status === 'mismatch' ||
    status === 'failed' ||
    status === 'connectivity_failed'
  const running = status === 'running'
  const warning =
    status === 'insufficient_evidence' || status === 'questionable'
  const Icon = passed
    ? CheckCircle2
    : failed
      ? XCircle
      : running
        ? Loader2
        : warning
          ? AlertTriangle
          : CircleDashed
  return (
    <span
      className={cn(
        'border-border bg-muted/30 inline-flex max-w-full min-w-0 items-center gap-1.5 rounded-sm border px-2 py-1 text-xs',
        passed && 'border-success/30 bg-success/8',
        failed && 'border-destructive/30 bg-destructive/8',
        warning && 'border-warning/35 bg-warning/8'
      )}
      title={`${props.model} · ${t(modelStatusLabel(status))}`}
    >
      <span className='max-w-40 min-w-0 truncate font-medium sm:max-w-56'>
        {props.model}
      </span>
      <span
        className={cn(
          'flex shrink-0 items-center gap-1',
          passed && 'text-success',
          failed && 'text-destructive',
          running && 'text-primary',
          warning && 'text-warning-foreground',
          !passed && !failed && !running && !warning && 'text-muted-foreground'
        )}
      >
        <Icon className={cn('size-3', running && 'animate-spin')} />
        {t(modelStatusLabel(status))}
      </span>
    </span>
  )
}

function modelResultStatus(
  group: MarketplaceGroup,
  model: string
): ModelResultStatus {
  const mapping = (group.gpt56_mapping_results ?? []).find((result) =>
    sameModel(result.requested_model, model)
  )
  if (mapping) return mapping.status
  if (hasGPT56Model([model]) && group.gpt56_mapping_status) {
    return group.gpt56_mapping_status
  }
  const connectivity = group.model_verification_results.find((result) =>
    sameModel(result.model, model)
  )
  if (connectivity) {
    return connectivity.status === 'passed'
      ? 'connectivity_passed'
      : 'connectivity_failed'
  }
  return group.model_consistency_status || ''
}

function modelStatusLabel(status: ModelResultStatus) {
  switch (status) {
    case 'matched':
    case 'passed':
      return '一致'
    case 'connectivity_passed':
      return '通过'
    case 'mismatch':
    case 'failed':
      return '不一致'
    case 'connectivity_failed':
      return '未通过'
    case 'insufficient_evidence':
      return '证据不足'
    case 'questionable':
      return '存疑'
    case 'running':
      return '检测中'
    default:
      return '未检测'
  }
}

function sameModel(left: string, right: string) {
  return left.trim().toLowerCase() === right.trim().toLowerCase()
}
