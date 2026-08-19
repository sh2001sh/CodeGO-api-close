import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  CirclePause,
  CircleDashed,
  Loader2,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { hasGPT56Model } from '../lib/verification'
import type {
  GPT56MappingLevel,
  GPT56MappingResult,
  GPT56MappingRun,
  GPT56MappingSample,
  GPT56MappingStatus,
  GPT56MappingTrigger,
} from '../types'
import { GPT56MappingHistory } from './gpt56-mapping-history'
import { mappingLevelLabel, mappingTriggerLabel } from './gpt56-mapping-labels'

const statusLabels: Record<GPT56MappingStatus, string> = {
  '': '未检测',
  queued: '等待检测',
  running: '检测中',
  matched: '映射正确',
  mismatch: '映射不一致',
  insufficient_evidence: '证据不足',
  paused: '已暂停',
}

export function GPT56MappingStatusView(props: {
  models: string[]
  status: GPT56MappingStatus
  results: GPT56MappingResult[]
  checkedAt?: string | null
  level?: GPT56MappingLevel | ''
  trigger?: GPT56MappingTrigger | ''
  history?: GPT56MappingRun[]
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
          {mappingExplanation(props.status, props.level, t)}
        </p>
        <div className='text-muted-foreground mt-1 flex flex-wrap gap-x-3 gap-y-1 tabular-nums'>
          {['queued', 'running', 'paused'].includes(props.status) && total > 0
            ? t('已完成 {{completed}} / {{total}} 次请求', {
                completed,
                total,
              })
            : props.checkedAt
              ? `${t('检测完成时间')}: ${new Date(props.checkedAt).toLocaleString()}`
              : t('等待首次检测')}
          {props.level && <span>{mappingLevelLabel(props.level, t)}</span>}
          {props.trigger && (
            <span>{mappingTriggerLabel(props.trigger, t)}</span>
          )}
        </div>
        {['queued', 'running'].includes(props.status) && total > 0 && (
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
      {props.history && props.history.length > 0 && (
        <GPT56MappingHistory runs={props.history} />
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
        : status === 'paused'
          ? CirclePause
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
        status === 'queued' && 'text-primary',
        status === 'paused' && 'text-muted-foreground',
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
        {sample.variant ? ` · ${sampleVariantLabel(sample.variant, t)}` : ''}
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
  level: GPT56MappingLevel | '' | undefined,
  t: (value: string) => string
) {
  if (status === 'matched') return t('本轮所有请求均返回了预期的模型标识。')
  if (status === 'mismatch')
    return level === 'daily_light'
      ? t('轻量检测发现异常，系统将自动进行确认检测，尚未据此下线。')
      : t('确认检测发现模型标识不一致，当前分组已停止参与路由。')
  if (status === 'insufficient_evidence') {
    return t(
      '本轮存在网络失败或缺少模型标识的请求，证据不足，不会据此自动下线。'
    )
  }
  if (status === 'running')
    return level === 'daily_light'
      ? t('正在执行每日轻量检测；发现异常后会自动进入确认检测。')
      : t('正在执行确认检测，结果会随请求完成自动更新。')
  if (status === 'queued') return t('检测已进入队列，即将开始。')
  if (status === 'paused')
    return t('检测已暂停，已完成的报告会保留；可重新开始检测。')
  return t('每日执行轻量检测；轻量异常会自动复检，只有确认异常才会下线。')
}

function sampleVariantLabel(variant: string, t: (value: string) => string) {
  const labels: Record<string, string> = {
    exact_reply: '精确回复',
    short_answer: '短回答',
    simple_fact: '简单事实',
  }
  return t(labels[variant] ?? variant)
}
