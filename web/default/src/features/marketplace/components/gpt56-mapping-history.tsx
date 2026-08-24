import { ChevronDown, Clock3 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { GPT56MappingRun, GPT56MappingStatus } from '../types'
import { mappingLevelLabel, mappingTriggerLabel } from './gpt56-mapping-labels'

const historyStatusLabels: Record<Exclude<GPT56MappingStatus, ''>, string> = {
  queued: '等待检测',
  running: '检测中',
  matched: '通过',
  mismatch: '不一致',
  insufficient_evidence: '证据不足',
  paused: '已暂停',
}

export function GPT56MappingHistory({ runs }: { runs: GPT56MappingRun[] }) {
  const { t } = useTranslation()
  return (
    <details className='group border-border border-t px-3 py-2.5'>
      <summary className='focus-visible:ring-ring flex cursor-pointer list-none items-center justify-between gap-3 rounded-sm outline-none focus-visible:ring-2'>
        <span className='flex items-center gap-1.5 font-medium'>
          <Clock3 className='size-3.5' />
          {t('最近检测记录')} ({runs.length})
        </span>
        <ChevronDown className='text-muted-foreground size-3.5 transition-transform duration-200 group-open:rotate-180 motion-reduce:transition-none' />
      </summary>
      <div className='divide-border mt-2 divide-y overflow-hidden rounded-sm border'>
        {runs.map((run) => {
          const finishedAt = run.completed_at ?? run.started_at
          return (
            <div
              key={run.id}
              className='grid gap-1 px-3 py-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:gap-4'
            >
              <div className='min-w-0'>
                <p className='font-medium'>
                  {mappingLevelLabel(run.level, t)} ·{' '}
                  {t(historyStatusLabels[run.status])}
                </p>
                <p className='text-muted-foreground mt-0.5'>
                  {mappingTriggerLabel(run.trigger, t)}
                  {run.parent_run_id ? ` · ${t('由轻量异常触发')}` : ''}
                </p>
              </div>
              <time
                className='text-muted-foreground tabular-nums sm:text-right'
                dateTime={finishedAt}
              >
                {new Date(finishedAt).toLocaleString()}
              </time>
            </div>
          )
        })}
      </div>
    </details>
  )
}
