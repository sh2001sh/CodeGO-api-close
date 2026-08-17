import { CheckCircle2, CircleHelp, Loader2, Minus, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { hasGPT56Model } from '../lib/verification'
import type {
  GPT56MappingResult,
  GPT56MappingStatus,
  ConnectivityTestStatus,
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
  models: string[]
  status: GPT56MappingStatus
  results: GPT56MappingResult[]
  checkedAt?: string | null
}) {
  const { t } = useTranslation()
  const status = props.status || ''
  if (!hasGPT56Model(props.models)) return null
  const tone =
    status === 'matched'
      ? 'text-success'
      : status === 'mismatch'
        ? 'text-destructive'
        : status === 'running'
          ? 'text-primary'
          : 'text-warning-foreground'
  return (
    <div className='border-border bg-muted/20 mt-3 rounded-md border px-3 py-2.5 text-xs'>
      <div className='flex flex-wrap items-center justify-between gap-x-3 gap-y-1'>
        <span className='font-medium'>{t('GPT-5.6 映射检测 · 必做')}</span>
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
              {result.requested_model}:{' '}
              {result.reported_model || t('未返回模型标识')}
              {result.sample_count > 0 && (
                <>
                  {' '}
                  · {result.matched_samples}/{result.sample_count}
                </>
              )}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

const connectivityLabels: Record<ConnectivityTestStatus, string> = {
  '': '未测试',
  queued: '等待测试',
  running: '测试中',
  passed: '测试通过',
  failed: '测试失败',
}

export function ConnectivityTestStatusView(props: {
  status: ConnectivityTestStatus
  results: ModelVerificationResult[]
  checkedAt?: string | null
  required: boolean
  showErrors?: boolean
}) {
  const { t } = useTranslation()
  const status = props.status || ''
  const running = status === 'queued' || status === 'running'
  const Icon =
    status === 'passed'
      ? CheckCircle2
      : status === 'failed'
        ? XCircle
        : running
          ? Loader2
          : Minus
  return (
    <div className='border-border bg-muted/20 mt-3 rounded-md border px-3 py-2.5 text-xs'>
      <div className='flex flex-wrap items-center justify-between gap-x-3 gap-y-1'>
        <span className='font-medium'>
          {t('模型连通性测试')} · {props.required ? t('必做') : t('可选')}
        </span>
        <span
          className={cn(
            'flex items-center gap-1 font-medium',
            status === 'passed' && 'text-success',
            status === 'failed' && 'text-destructive',
            running && 'text-primary',
            status === '' && 'text-muted-foreground'
          )}
        >
          <Icon className={cn('size-3.5', running && 'animate-spin')} />
          {t(connectivityLabels[status])}
        </span>
      </div>
      <div className='text-muted-foreground mt-1'>
        {props.checkedAt
          ? `${t('最近测试')}: ${new Date(props.checkedAt).toLocaleString()}`
          : props.required
            ? t('等待首次测试')
            : t('GPT-5.6 检测通过后可跳过此项')}
      </div>
      <ModelConnectivityResults
        results={props.results}
        showErrors={props.showErrors}
        embedded
      />
    </div>
  )
}

export function AutoProbeStatusView(props: {
  enabled: boolean
  intervalMinutes: number
  model: string
  status: ConnectivityTestStatus
  checkedAt?: string | null
}) {
  const { t } = useTranslation()
  if (!props.enabled) return null
  return (
    <div className='border-border bg-muted/20 mt-3 rounded-md border px-3 py-2.5 text-xs'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <span className='font-medium'>{t('自动探针')}</span>
        <span className={props.status === 'passed' ? 'text-success font-medium' : props.status === 'failed' ? 'text-destructive font-medium' : 'text-muted-foreground'}>
          {props.status === 'passed' ? t('通过') : props.status === 'failed' ? t('失败') : t('等待首次探测')}
        </span>
      </div>
      <p className='text-muted-foreground mt-1'>
        {t('{{model}} · 每 {{minutes}} 分钟', { model: props.model, minutes: props.intervalMinutes })}
        {props.checkedAt ? ` · ${new Date(props.checkedAt).toLocaleString()}` : ''}
      </p>
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

export function ModelConsistencyStatusView(props: {
  status: ModelConsistencyStatus
  checkedAt?: string | null
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap items-center gap-2'>
      <ModelConsistencyBadge status={props.status} />
      <span className='text-muted-foreground text-xs tabular-nums'>
        {props.checkedAt
          ? t('检测于 {{time}}', {
              time: new Date(props.checkedAt).toLocaleString(),
            })
          : t('暂无检测时间')}
      </span>
    </div>
  )
}

export function ModelConnectivityResults(props: {
  results: ModelVerificationResult[]
  showErrors?: boolean
  embedded?: boolean
}) {
  const { t } = useTranslation()
  if (props.results.length === 0) return null
  return (
    <div
      className={cn(
        'text-foreground mt-3 overflow-hidden',
        props.embedded ? 'border-border border-t' : 'rounded-md border'
      )}
    >
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
            <span className='text-destructive break-all'>
              {props.result.error}
            </span>
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
        {props.result.latency_ms > 0 && (
          <span>· {props.result.latency_ms}ms</span>
        )}
      </div>
    </div>
  )
}
