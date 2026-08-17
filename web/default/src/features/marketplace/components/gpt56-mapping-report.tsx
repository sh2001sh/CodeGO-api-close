import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  CircleDashed,
  Loader2,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { hasGPT56Model } from '../lib/verification'
import type {
  GPT56MappingResult,
  GPT56MappingSample,
  GPT56MappingStatus,
} from '../types'

const statusLabels: Record<GPT56MappingStatus, string> = {
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
  embedded?: boolean
}) {
  const { t } = useTranslation()
  if (!hasGPT56Model(props.models)) return null
  const completed = props.results.reduce(
    (sum, result) => sum + (result.samples?.length ?? 0),
    0
  )
  const total = props.results.reduce(
    (sum, result) => sum + result.sample_count,
    0
  )
  return (
    <div
      className={cn(
        'overflow-hidden text-xs',
        props.embedded
          ? 'mt-2'
          : 'border-border bg-muted/20 mt-3 rounded-md border'
      )}
    >
      <div className='px-3 py-2.5'>
        <div className='flex flex-wrap items-center justify-between gap-x-3 gap-y-1'>
          <span className='font-medium'>{t('GPT-5.6 映射检测')}</span>
          <MappingStatus status={props.status} />
        </div>
        <p className='text-muted-foreground mt-1 leading-5'>
          {mappingExplanation(props.status, t)}
        </p>
        <p className='text-muted-foreground mt-1 tabular-nums'>
          {props.status === 'running' && total > 0
            ? t('已完成 {{completed}} / {{total}} 次探测', {
                completed,
                total,
              })
            : props.checkedAt
              ? `${t('最近检测')}: ${new Date(props.checkedAt).toLocaleString()}`
              : t('等待首次检测')}
        </p>
        {props.status === 'running' && total > 0 && (
          <div
            className='bg-muted mt-2 h-1.5 overflow-hidden rounded-full'
            role='progressbar'
            aria-valuemin={0}
            aria-valuemax={total}
            aria-valuenow={completed}
          >
            <div
              className='bg-primary h-full rounded-full transition-[width] duration-200 motion-reduce:transition-none'
              style={{ width: `${Math.min(100, (completed / total) * 100)}%` }}
            />
          </div>
        )}
      </div>
      {props.results.length > 0 && (
        <div className='border-border divide-border divide-y border-t'>
          {props.results
            .filter((result) => result.requested_model)
            .map((result) => (
              <MappingModelReport
                key={result.requested_model}
                result={result}
              />
            ))}
        </div>
      )}
    </div>
  )
}

function MappingStatus({ status }: { status: GPT56MappingStatus }) {
  const { t } = useTranslation()
  const Icon =
    status === 'matched'
      ? CheckCircle2
      : status === 'mismatch'
        ? XCircle
        : status === 'running'
          ? Loader2
          : status === 'insufficient_evidence'
            ? AlertTriangle
            : CircleDashed
  return (
    <span
      className={cn(
        'flex items-center gap-1 font-medium',
        status === 'matched' && 'text-success',
        status === 'mismatch' && 'text-destructive',
        status === 'running' && 'text-primary',
        status === 'insufficient_evidence' && 'text-warning-foreground',
        status === '' && 'text-muted-foreground'
      )}
    >
      <Icon
        className={cn('size-3.5', status === 'running' && 'animate-spin')}
      />
      {t(statusLabels[status])}
    </span>
  )
}

function MappingModelReport({ result }: { result: GPT56MappingResult }) {
  const { t } = useTranslation()
  const samples = result.samples ?? []
  const waiting = result.status === 'running' && samples.length === 0
  return (
    <details className='group px-3 py-2.5'>
      <summary className='focus-visible:ring-ring flex cursor-pointer list-none items-center justify-between gap-3 rounded-sm outline-none focus-visible:ring-2'>
        <div className='min-w-0'>
          <p className='truncate font-medium' title={result.requested_model}>
            {result.requested_model}
          </p>
          <p className='text-muted-foreground mt-0.5 tabular-nums'>
            {result.matched_samples}/{result.sample_count} {t('次返回一致')}
            {result.latency_ms > 0 && ` · ${t('平均')} ${result.latency_ms}ms`}
          </p>
        </div>
        <span className='text-muted-foreground flex shrink-0 items-center gap-1'>
          {samples.length > 0
            ? t('查看 {{count}} 次报告', { count: samples.length })
            : waiting
              ? t('等待探测')
              : t('查看说明')}
          <ChevronDown className='size-3.5 transition-transform duration-200 group-open:rotate-180 motion-reduce:transition-none' />
        </span>
      </summary>
      <div className='mt-2'>
        {samples.length > 0 ? (
          <div className='divide-border overflow-hidden rounded-sm border'>
            {samples.map((sample) => (
              <MappingSampleRow key={sample.index} sample={sample} />
            ))}
          </div>
        ) : (
          <p className='text-muted-foreground bg-muted/30 rounded-sm px-3 py-2 leading-5'>
            {waiting
              ? t('前面的模型完成后将自动开始该模型的探测。')
              : result.error
                ? t('未生成逐次报告：{{error}}', { error: result.error })
                : t('当前结果来自旧版汇总记录，重新检测后可查看逐次请求报告。')}
          </p>
        )}
      </div>
    </details>
  )
}

function MappingSampleRow({ sample }: { sample: GPT56MappingSample }) {
  const { t } = useTranslation()
  const passed = sample.status === 'matched'
  const mismatched = sample.status === 'mismatch'
  const Icon = passed ? CheckCircle2 : mismatched ? XCircle : AlertTriangle
  return (
    <div className='grid gap-1 px-3 py-2 sm:grid-cols-[72px_minmax(0,1fr)_auto] sm:items-start sm:gap-3'>
      <span className='text-muted-foreground tabular-nums'>
        {t('第 {{index}} 次', { index: sample.index })}
      </span>
      <div className='min-w-0'>
        <p
          className={cn(
            'font-medium break-all',
            passed && 'text-success',
            mismatched && 'text-destructive',
            !passed && !mismatched && 'text-warning-foreground'
          )}
        >
          <Icon className='mr-1 inline size-3.5 align-[-2px]' />
          {sample.reported_model || t('未获得模型标识')}
        </p>
        {sample.error && (
          <p className='text-destructive mt-0.5 leading-5 break-all'>
            {sample.error}
          </p>
        )}
      </div>
      <span className='text-muted-foreground tabular-nums sm:text-right'>
        {sample.latency_ms}ms
        {sample.tested_at
          ? ` · ${new Date(sample.tested_at).toLocaleTimeString()}`
          : ''}
      </span>
    </div>
  )
}

function mappingExplanation(
  status: GPT56MappingStatus,
  t: (value: string) => string
) {
  if (status === 'matched')
    return t('所有模型的 3 次探测均返回请求的模型标识。')
  if (status === 'mismatch') return t('至少一次探测返回了不同的模型标识。')
  if (status === 'insufficient_evidence') {
    return t(
      '没有发现错误映射，但存在失败或缺少模型标识的样本；每个模型必须 3/3 一致才算通过。'
    )
  }
  if (status === 'running')
    return t('正在逐模型执行 3 次独立探测，结果会自动更新。')
  return t('每个 GPT-5.6 模型会连续探测 3 次并核对上游返回的模型标识。')
}
