import { Check, RefreshCcw, RotateCcw, Search, UserPlus, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import type { AdminOwnerIncomeItem, AdminOwnerIncomeResult } from '../types'

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
  selectedOwners: AdminOwnerIncomeItem[]
  onToggleOwner: (owner: AdminOwnerIncomeItem) => void
  onAddVisible: () => void
  onClearSelected: () => void
}) {
  const { t } = useTranslation()
  const visibleOwners = props.report?.items ?? []
  const selectedIDs = new Set(
    props.selectedOwners.map((owner) => owner.owner_user_id)
  )
  const addableCount = visibleOwners.filter(
    (owner) => !selectedIDs.has(owner.owner_user_id)
  ).length

  return (
    <div className='grid gap-4 xl:grid-cols-[minmax(0,1.45fr)_minmax(20rem,0.55fr)]'>
      <section className='border-border overflow-hidden rounded-md border'>
        <div className='bg-muted/15 border-border border-b p-4'>
          <div className='flex flex-col gap-3 lg:flex-row lg:items-end'>
            <label className='min-w-0 flex-1 space-y-1.5'>
              <span className='text-sm font-medium'>{t('搜索渠道主')}</span>
              <div className='relative'>
                <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
                <Input
                  value={props.ownerSearch}
                  onChange={(event) =>
                    props.onOwnerSearchChange(event.currentTarget.value)
                  }
                  placeholder={t('输入渠道主外部 ID')}
                  aria-label={t('搜索渠道主外部 ID')}
                  className='pl-9'
                />
              </div>
            </label>
            <div className='space-y-1.5'>
              <span className='text-sm font-medium'>{t('收益时间')}</span>
              <div className='flex gap-2'>
                <CompactDateTimeRangePicker
                  start={props.range.start}
                  end={props.range.end}
                  onChange={props.onRangeChange}
                  className='w-full sm:w-[19rem]'
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
              </div>
            </div>
            <Button
              variant='outline'
              size='icon'
              onClick={props.onRefresh}
              disabled={props.isFetching}
              title={t('刷新收益')}
              aria-label={t('刷新收益')}
            >
              <RefreshCcw className={props.isFetching ? 'animate-spin' : ''} />
            </Button>
          </div>
        </div>

        <div className='flex min-h-12 items-center justify-between gap-3 px-4 py-2'>
          <p className='text-muted-foreground text-sm'>
            {props.isFetching
              ? t('正在查询渠道主收益…')
              : t('找到 {{count}} 位渠道主', { count: visibleOwners.length })}
          </p>
          {visibleOwners.length > 0 && (
            <Button
              variant='outline'
              size='sm'
              onClick={props.onAddVisible}
              disabled={props.isFetching || addableCount === 0}
            >
              <UserPlus />
              {addableCount === 0
                ? t('当前结果已全部选择')
                : t('添加当前结果（{{count}}）', { count: addableCount })}
            </Button>
          )}
        </div>

        {props.isError && (
          <div className='bg-destructive/5 text-destructive border-border border-t px-4 py-3 text-sm'>
            {t('渠道主收益加载失败，请重试。')}
          </div>
        )}
        {!props.isError && !props.isFetching && visibleOwners.length === 0 && (
          <div className='border-border border-t px-4 py-10 text-center'>
            <Search className='text-muted-foreground/60 mx-auto size-5' />
            <p className='text-muted-foreground mt-2 text-sm'>
              {t('当前条件下没有可选择的渠道主')}
            </p>
          </div>
        )}
        {!props.isError && visibleOwners.length > 0 && (
          <div className='border-border max-h-72 divide-y overflow-y-auto border-t'>
            {visibleOwners.map((owner) => {
              const selected = selectedIDs.has(owner.owner_user_id)
              return (
                <button
                  type='button'
                  key={owner.owner_user_id}
                  onClick={() => props.onToggleOwner(owner)}
                  disabled={props.isFetching}
                  className='hover:bg-muted/40 focus-visible:ring-ring grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-4 px-4 py-3 text-left focus-visible:ring-2 focus-visible:outline-none'
                >
                  <span className='min-w-0'>
                    <span className='block truncate font-mono text-sm font-semibold'>
                      {owner.owner_external_id || owner.owner_user_id}
                    </span>
                    <span className='text-muted-foreground mt-0.5 block text-xs'>
                      {t('已到账 {{released}} · 当前余额 {{balance}}', {
                        released: formatQuota(owner.released_income),
                        balance: formatQuota(owner.current_quota),
                      })}
                    </span>
                  </span>
                  <span
                    className={
                      selected
                        ? 'bg-primary text-primary-foreground flex size-7 items-center justify-center rounded-sm'
                        : 'border-border text-muted-foreground flex size-7 items-center justify-center rounded-sm border'
                    }
                  >
                    {selected ? (
                      <Check className='size-4' />
                    ) : (
                      <UserPlus className='size-4' />
                    )}
                  </span>
                </button>
              )
            })}
          </div>
        )}
        <div className='text-muted-foreground border-border border-t px-4 py-2 text-xs'>
          {t('收益按历史结算记录统计，渠道删除后仍会保留。')}
        </div>
      </section>

      <section className='border-border overflow-hidden rounded-md border'>
        <div className='bg-muted/15 border-border flex min-h-14 items-center justify-between gap-3 border-b px-4 py-3'>
          <div>
            <h3 className='text-sm font-semibold'>{t('本次回收对象')}</h3>
            <p className='text-muted-foreground text-xs'>
              {t('已选择 {{count}} 位渠道主', {
                count: props.selectedOwners.length,
              })}
            </p>
          </div>
          {props.selectedOwners.length > 0 && (
            <Button variant='ghost' size='sm' onClick={props.onClearSelected}>
              {t('清空')}
            </Button>
          )}
        </div>
        {props.selectedOwners.length === 0 ? (
          <p className='text-muted-foreground px-4 py-10 text-center text-sm'>
            {t('从左侧搜索结果中添加渠道主')}
          </p>
        ) : (
          <div className='max-h-72 divide-y overflow-y-auto'>
            {props.selectedOwners.map((owner) => (
              <div
                key={owner.owner_user_id}
                className='grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-4 py-3'
              >
                <div className='min-w-0'>
                  <p className='truncate font-mono text-sm font-semibold'>
                    {owner.owner_external_id || owner.owner_user_id}
                  </p>
                  <p className='text-muted-foreground text-xs tabular-nums'>
                    {t('可回收 {{amount}}', {
                      amount: formatQuota(owner.reclaimable_quota),
                    })}
                  </p>
                </div>
                <Button
                  variant='ghost'
                  size='icon-sm'
                  onClick={() => props.onToggleOwner(owner)}
                  title={t('移除渠道主')}
                  aria-label={t('移除渠道主')}
                >
                  <X />
                </Button>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
