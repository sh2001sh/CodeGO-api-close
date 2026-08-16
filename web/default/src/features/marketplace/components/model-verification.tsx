import { CheckCircle2, CircleHelp, Minus, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import type {
	GPT56MappingResult,
	GPT56MappingStatus,
	ModelConsistencyStatus,
	ModelVerificationResult,
} from '../types'

const consistencyLabels: Record<ModelConsistencyStatus, string> = {
  '': '暂无',
  passed: '通过',
  failed: '不通过',
  questionable: '存疑',
}

const mappingLabels: Record<GPT56MappingStatus, string> = {
  '': '未检测',
  running: '检测中',
  matched: '映射正确',
  mismatch: '映射不一致',
  insufficient_evidence: '证据不足',
}

export function GPT56MappingStatusView(props: {
  sourceLabel: string
  models: string[]
  status: GPT56MappingStatus
  results: GPT56MappingResult[]
  checkedAt?: string | null
}) {
  const { t } = useTranslation()
  const status = props.status || ''
  const eligible =
    ['Codex Plus', 'Codex Pro'].includes(props.sourceLabel) &&
    ['gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna'].some((model) =>
      props.models.some((item) => item.toLowerCase() === model)
    )
  if (!eligible) return null
  const tone =
    status === 'matched'
      ? 'text-success'
      : status === 'mismatch'
        ? 'text-destructive'
        : status === 'running'
          ? 'text-primary'
          : 'text-warning-foreground'
  return (
    <div className='mt-3 border-border bg-muted/20 rounded-md border px-3 py-2.5 text-xs'>
      <div className='flex flex-wrap items-center justify-between gap-x-3 gap-y-1'>
        <span className='font-medium'>{t('GPT-5.6 映射检测')}</span>
        <span className={cn('font-medium', tone)}>
          {t(mappingLabels[status])}
        </span>
      </div>
      <div className='text-muted-foreground mt-1'>
        {props.checkedAt
          ? `${t('最近检测')}: ${new Date(props.checkedAt).toLocaleString()}`
          : t('等待首次检测')}
      </div>
      {props.results.length > 0 && (
        <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-3 gap-y-1'>
          {props.results.map((result) => (
            <span key={result.requested_model}>
              {result.requested_model}: {result.reported_model || t('未返回模型标识')}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

const consistencyIcons = {
  '': Minus,
  passed: CheckCircle2,
  failed: XCircle,
  questionable: CircleHelp,
} as const

export function ModelConsistencyBadge(props: {
  status: ModelConsistencyStatus
}) {
  const { t } = useTranslation()
  const status = props.status || ''
  const Icon = consistencyIcons[status]
  return (
    <Badge
      variant='outline'
      className={cn(
        'gap-1 font-normal',
        status === 'passed' && 'border-success/35 bg-success/10 text-success',
        status === 'failed' &&
          'border-destructive/35 bg-destructive/10 text-destructive',
        status === 'questionable' &&
          'border-warning/35 bg-warning/10 text-warning-foreground',
        status === '' && 'text-muted-foreground'
      )}
    >
      <Icon className='size-3' />
      {t('模型一致性')}: {t(consistencyLabels[status])}
    </Badge>
  )
}

export function ModelConnectivityResults(props: {
  results: ModelVerificationResult[]
  showErrors?: boolean
}) {
  const { t } = useTranslation()
  if (props.results.length === 0) return null
  return (
    <div className='text-foreground mt-3 overflow-hidden rounded-md border'>
      <div className='bg-muted/30 text-muted-foreground grid grid-cols-[minmax(0,1fr)_auto] gap-3 px-3 py-2 text-[11px] font-medium'>
        <span>{t('逐模型连通性')}</span>
        <span>{t('结果')}</span>
      </div>
      <div className='divide-border divide-y'>
        {props.results.map((result) => (
          <ModelConnectivityRow
            key={result.model}
            result={result}
            showError={props.showErrors === true}
          />
        ))}
      </div>
    </div>
  )
}

function ModelConnectivityRow(props: {
  result: ModelVerificationResult
  showError: boolean
}) {
  const { t } = useTranslation()
  const passed = props.result.status === 'passed'
  const Icon = passed ? CheckCircle2 : XCircle
  return (
    <div className='grid grid-cols-[minmax(0,1fr)_auto] gap-3 px-3 py-2.5 text-xs'>
      <div className='min-w-0'>
        <div className='truncate font-medium' title={props.result.model}>
          {props.result.model}
        </div>
        <div className='text-muted-foreground mt-0.5 flex flex-wrap gap-x-2 gap-y-0.5 text-[11px]'>
          <span>
            {props.result.listed ? t('上游列表已发现') : t('上游列表未发现')}
          </span>
          {props.showError && props.result.error && (
            <span className='text-destructive break-all'>{props.result.error}</span>
          )}
        </div>
      </div>
      <div
        className={cn(
          'flex items-center gap-1 self-start font-medium tabular-nums',
          passed ? 'text-success' : 'text-destructive'
        )}
      >
        <Icon className='size-3.5' />
        <span>{passed ? t('通过') : t('失败')}</span>
        {props.result.latency_ms > 0 && <span>· {props.result.latency_ms}ms</span>}
      </div>
    </div>
  )
}
