import { RefreshCcw, RotateCcw, Search, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import type { AdminOwnerIncomeResult } from '../types'

export interface AdminIncomeRange {
  start?: Date
  end?: Date
}

export function AdminIncomeFilter(props: {
  report?: AdminOwnerIncomeResult
  ownerSearch: string
  onOwnerSearchChange: (value: string) => void
  range: AdminIncomeRange
  onRangeChange: (range: AdminIncomeRange) => void
  onRefresh: () => void
  isFetching: boolean
  isError: boolean
  onRelease: () => void
  releasing: boolean
}) {
  const { t } = useTranslation()
  const report = props.report

  return (
    <section className='border-border overflow-hidden rounded-md border'>
      <div className='bg-muted/15 flex flex-col gap-3 px-4 py-3 lg:flex-row lg:items-center lg:justify-between'>
        <div className='flex flex-wrap gap-x-5 gap-y-2 text-sm'>
          <IncomeValue
            label={t('渠道主')}
            value={(report?.owner_count ?? 0).toLocaleString()}
          />
          <IncomeValue
            label={t('筛选收益')}
            value={formatQuota(report?.total_income ?? 0)}
          />
          <IncomeValue
            label={t('待结算')}
            value={formatQuota(report?.pending_income ?? 0)}
          />
          <IncomeValue
            label={t('已到账收益')}
            value={formatQuota(report?.released_income ?? 0)}
          />
        </div>
        <div className='flex flex-col gap-2 sm:flex-row sm:items-center'>
          <div className='relative w-full sm:w-48'>
            <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
            <Input
              value={props.ownerSearch}
              onChange={(event) =>
                props.onOwnerSearchChange(event.currentTarget.value)
              }
              placeholder={t('搜索渠道主外部 ID')}
              aria-label={t('搜索渠道主外部 ID')}
              className='pl-9'
            />
          </div>
          <CompactDateTimeRangePicker
            start={props.range.start}
            end={props.range.end}
            onChange={props.onRangeChange}
            className='w-full sm:w-[18.5rem]'
          />
          {(props.range.start || props.range.end) && (
            <Button
              variant='outline'
              size='icon'
              onClick={() => props.onRangeChange({})}
              title={t('清除时间范围')}
              aria-label={t('清除时间范围')}
            >
              <RotateCcw />
            </Button>
          )}
          <Button
            variant='outline'
            size='icon'
            onClick={props.onRefresh}
            disabled={props.isFetching || props.releasing}
            title={t('刷新收益')}
            aria-label={t('刷新收益')}
          >
            <RefreshCcw className={props.isFetching ? 'animate-spin' : ''} />
          </Button>
          <Button
            onClick={props.onRelease}
            disabled={
              props.releasing ||
              props.isFetching ||
              (report?.pending_income ?? 0) <= 0
            }
            title={t('按当前用户和时间范围结算待结算收益')}
          >
            <WalletCards className={props.releasing ? 'animate-pulse' : ''} />
            {props.releasing ? t('结算中') : t('立即结算待结算收益')}
          </Button>
        </div>
      </div>
      {props.isError && (
        <div className='bg-destructive/5 text-destructive border-border border-t px-4 py-2 text-xs'>
          {t('渠道主收益加载失败，请重试。')}
        </div>
      )}
      {!props.isError && (report?.items.length ?? 0) > 0 && (
        <div className='border-border max-h-52 overflow-y-auto border-t'>
          {report?.items.map((item) => (
            <div
              key={item.owner_user_id}
              className='border-border grid grid-cols-2 gap-x-4 gap-y-1 border-b px-4 py-2.5 text-xs last:border-b-0 sm:grid-cols-[minmax(8rem,1fr)_repeat(4,minmax(6rem,auto))] sm:items-center'
            >
              <span className='text-foreground font-medium tabular-nums'>
                {t('渠道主 ID')}: {item.owner_external_id || '--'}
              </span>
              <ReportValue
                label={t('收益')}
                value={formatQuota(item.total_income)}
              />
              <ReportValue
                label={t('待结算')}
                value={formatQuota(item.pending_income)}
              />
              <ReportValue
                label={t('已到账收益')}
                value={formatQuota(item.released_income)}
              />
              <ReportValue
                label={t('请求')}
                value={item.request_count.toLocaleString()}
              />
            </div>
          ))}
        </div>
      )}
      <div className='text-muted-foreground border-border border-t px-4 py-2 text-xs'>
        {t('收益按历史结算记录统计，渠道删除后仍会保留。')}
      </div>
    </section>
  )
}

function ReportValue(props: { label: string; value: string }) {
  return (
    <span className='text-muted-foreground tabular-nums'>
      {props.label}: <strong className='text-foreground'>{props.value}</strong>
    </span>
  )
}

function IncomeValue(props: { label: string; value: string }) {
  return (
    <div className='min-w-24'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='mt-0.5 font-semibold tabular-nums'>{props.value}</div>
    </div>
  )
}
