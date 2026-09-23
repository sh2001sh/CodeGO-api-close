import { useRef, useState } from 'react'
import { Download, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  formatQuota,
  parseQuotaFromDollars,
  quotaUnitsToDollars,
} from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useAdminOwnerIncome, useAdminOwnerIncomeRelease } from '../hooks'
import type {
  AdminOwnerIncomeItem,
  AdminOwnerIncomeReleaseResult,
} from '../types'
import { AdminIncomeFilter, type AdminIncomeRange } from './admin-income-filter'

export function AdminOwnerIncomePanel(props: {
  ownerSearch: string
  onOwnerSearchChange: (value: string) => void
  range: AdminIncomeRange
  onRangeChange: (range: AdminIncomeRange) => void
}) {
  const { t } = useTranslation()
  const [selectedOwners, setSelectedOwners] = useState<AdminOwnerIncomeItem[]>(
    []
  )
  const [amount, setAmount] = useState('')
  const [lastResult, setLastResult] =
    useState<AdminOwnerIncomeReleaseResult | null>(null)
  const pendingOperation = useRef<{ signature: string; id: string } | null>(
    null
  )
  const filters = {
    ownerSearch: props.ownerSearch.trim(),
    startTimestamp:
      props.range.start && Math.floor(props.range.start.getTime() / 1000),
    endTimestamp:
      props.range.end && Math.floor(props.range.end.getTime() / 1000),
  }
  const query = useAdminOwnerIncome(filters)
  const reclaim = useAdminOwnerIncomeRelease()
  const available = selectedOwners.reduce(
    (sum, owner) => sum + owner.reclaimable_quota,
    0
  )

  const toggleOwner = (owner: AdminOwnerIncomeItem) => {
    setSelectedOwners((current) => {
      const exists = current.some(
        (item) => item.owner_user_id === owner.owner_user_id
      )
      return exists
        ? current.filter((item) => item.owner_user_id !== owner.owner_user_id)
        : [...current, owner]
    })
  }

  const addVisibleOwners = () => {
    setSelectedOwners((current) => {
      const next = new Map(current.map((owner) => [owner.owner_user_id, owner]))
      for (const owner of query.data?.items ?? []) {
        next.set(owner.owner_user_id, owner)
      }
      return Array.from(next.values())
    })
  }

  const changeRange = (range: AdminIncomeRange) => {
    setSelectedOwners([])
    setLastResult(null)
    props.onRangeChange(range)
  }

  const submit = () => {
    if (
      query.isFetching ||
      query.isError ||
      reclaim.isPending ||
      selectedOwners.length === 0 ||
      available <= 0
    )
      return
    const partial = amount.trim() !== ''
    const maxAmount = partial
      ? parseQuotaFromDollars(Number(amount))
      : undefined
    if (
      partial &&
      (!Number.isFinite(Number(amount)) ||
        !Number.isSafeInteger(maxAmount) ||
        !maxAmount ||
        maxAmount <= 0)
    ) {
      toast.error(t('请输入有效的正数回收金额'))
      return
    }
    if (maxAmount && maxAmount > available) {
      toast.error(t('输入金额超过所选渠道主当前可回收额度'))
      return
    }
    const scope = `${props.range.start?.toLocaleString() ?? t('不限开始时间')} — ${props.range.end?.toLocaleString() ?? t('不限结束时间')}`
    if (
      !window.confirm(
        t(
          '将从 {{count}} 位渠道主的可用额度中回收{{amount}}。\n收益时间：{{scope}}\n渠道主：{{owners}}',
          {
            count: selectedOwners.length,
            amount: formatQuota(maxAmount ?? available),
            scope,
            owners: selectedOwners
              .map((owner) => owner.owner_external_id || owner.owner_user_id)
              .join(', '),
          }
        )
      )
    )
      return
    const ownerUserIds = selectedOwners
      .map((owner) => owner.owner_user_id)
      .sort((a, b) => a - b)
    const releaseFilters = {
      startTimestamp: filters.startTimestamp,
      endTimestamp: filters.endTimestamp,
      ownerUserIds,
      maxAmount,
    }
    const signature = JSON.stringify(releaseFilters)
    if (pendingOperation.current?.signature !== signature) {
      pendingOperation.current = { signature, id: crypto.randomUUID() }
    }
    reclaim.mutate(
      {
        ...releaseFilters,
        operationId: pendingOperation.current.id,
      },
      {
        onSuccess: (result) => {
          pendingOperation.current = null
          setLastResult(result)
          toast.success(
            t('实际回收 {{count}} 条收益，共 {{amount}}', {
              count: result.reclaimed_count,
              amount: formatQuota(result.reclaimed_amount),
            })
          )
          setSelectedOwners([])
          setAmount('')
        },
        onError: (error) =>
          toast.error(
            error instanceof Error ? error.message : t('额度回收失败')
          ),
      }
    )
  }

  const exportResult = () => {
    if (!lastResult?.items.length) return
    const rows = lastResult.items.map((item) => [
      item.owner_external_id || String(item.owner_user_id),
      String(quotaUnitsToDollars(item.amount)),
    ])
    const csv = [[t('渠道主 ID'), t('回收额度')], ...rows]
      .map((row) => row.map(escapeCSVCell).join(','))
      .join('\r\n')
    const url = URL.createObjectURL(
      new Blob([`\uFEFF${csv}`], { type: 'text/csv;charset=utf-8' })
    )
    const link = document.createElement('a')
    link.href = url
    link.download = `marketplace-reclaim-${new Date().toISOString().replace(/[:.]/g, '-')}.csv`
    link.click()
    URL.revokeObjectURL(url)
  }

  return (
    <section aria-label={t('渠道主收益管理')} className='my-5 space-y-4'>
      <div className='flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between'>
        <div>
          <p className='text-muted-foreground text-xs font-semibold tracking-wider uppercase'>
            {t('批量财务操作')}
          </p>
          <h2 className='mt-1 text-xl font-semibold'>
            {t('批量回收渠道主收益')}
          </h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('按时间查找渠道主，加入回收批次后统一执行。')}
          </p>
        </div>
        <div className='text-muted-foreground text-sm tabular-nums'>
          {t('当前筛选已到账')}{' '}
          <strong className='text-foreground'>
            {formatQuota(query.data?.released_income ?? 0)}
          </strong>
        </div>
      </div>

      <AdminIncomeFilter
        report={query.data}
        ownerSearch={props.ownerSearch}
        onOwnerSearchChange={props.onOwnerSearchChange}
        range={props.range}
        onRangeChange={changeRange}
        onRefresh={() => void query.refetch()}
        isFetching={query.isFetching}
        isError={query.isError}
        selectedOwners={selectedOwners}
        onToggleOwner={toggleOwner}
        onAddVisible={addVisibleOwners}
        onClearSelected={() => setSelectedOwners([])}
      />

      <section className='border-primary/25 bg-primary/[0.025] rounded-md border p-4'>
        <div className='grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem_auto] lg:items-end'>
          <div>
            <h3 className='font-semibold'>{t('执行批量回收')}</h3>
            <p className='text-muted-foreground mt-1 text-sm'>
              {t('已选 {{count}} 人，可回收 {{amount}}。留空金额将全部回收。', {
                count: selectedOwners.length,
                amount: formatQuota(available),
              })}
            </p>
          </div>
          <label className='space-y-1.5'>
            <span className='text-sm font-medium'>{t('本次回收总额')}</span>
            <Input
              type='number'
              min='0'
              step='any'
              placeholder={t('留空则全部回收')}
              value={amount}
              onChange={(event) => setAmount(event.currentTarget.value)}
              aria-label={t('本次回收总额')}
            />
          </label>
          <Button
            onClick={submit}
            disabled={
              reclaim.isPending ||
              query.isError ||
              query.isFetching ||
              selectedOwners.length === 0 ||
              available <= 0
            }
            className='lg:min-w-40'
          >
            <WalletCards className={reclaim.isPending ? 'animate-pulse' : ''} />
            {reclaim.isPending ? t('回收中') : t('确认批量回收')}
          </Button>
        </div>
      </section>

      {lastResult && (
        <section className='border-border overflow-hidden rounded-md border'>
          <div className='bg-muted/15 border-border flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between'>
            <div>
              <h3 className='font-semibold'>{t('最近一次回收结果')}</h3>
              <p className='text-muted-foreground text-sm'>
                {t('{{count}} 位渠道主，共回收 {{amount}}', {
                  count: lastResult.items.length,
                  amount: formatQuota(lastResult.reclaimed_amount),
                })}
              </p>
            </div>
            <Button
              variant='outline'
              onClick={exportResult}
              disabled={lastResult.items.length === 0}
            >
              <Download />
              {t('导出 CSV')}
            </Button>
          </div>
          <div className='overflow-x-auto'>
            <table className='w-full text-sm'>
              <thead className='bg-muted/10 text-muted-foreground'>
                <tr>
                  <th className='px-4 py-2.5 text-left font-medium'>
                    {t('渠道主 ID')}
                  </th>
                  <th className='px-4 py-2.5 text-right font-medium'>
                    {t('回收额度')}
                  </th>
                </tr>
              </thead>
              <tbody className='divide-border divide-y'>
                {lastResult.items.map((item) => (
                  <tr key={item.owner_user_id}>
                    <td className='px-4 py-3 font-mono font-medium'>
                      {item.owner_external_id || item.owner_user_id}
                    </td>
                    <td className='px-4 py-3 text-right font-semibold tabular-nums'>
                      {formatQuota(item.amount)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </section>
  )
}

function escapeCSVCell(value: string) {
  return `"${value.replaceAll('"', '""')}"`
}
