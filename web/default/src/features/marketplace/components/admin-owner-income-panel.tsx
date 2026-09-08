import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { useAdminOwnerIncome, useAdminOwnerIncomeRelease } from '../hooks'
import { AdminIncomeFilter, type AdminIncomeRange } from './admin-income-filter'

export function AdminOwnerIncomePanel(props: {
  ownerSearch: string
  onOwnerSearchChange: (value: string) => void
  range: AdminIncomeRange
  onRangeChange: (range: AdminIncomeRange) => void
}) {
  const { t } = useTranslation()
  const [selectedIDs, setSelectedIDs] = useState<number[]>([])
  const [amount, setAmount] = useState('')
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
  const selected = (query.data?.items ?? []).filter((item) =>
    selectedIDs.includes(item.owner_user_id)
  )
  const available = selected.reduce(
    (sum, item) => sum + item.reclaimable_quota,
    0
  )

  const submit = () => {
    if (
      query.isFetching ||
      query.isError ||
      reclaim.isPending ||
      !selected.length ||
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
            count: selected.length,
            amount: formatQuota(maxAmount ?? available),
            scope,
            owners: selected
              .map((item) => item.owner_external_id || item.owner_user_id)
              .join(', '),
          }
        )
      )
    )
      return
    const ownerUserIds = selected
      .map((item) => item.owner_user_id)
      .sort((a, b) => a - b)
    const signature = JSON.stringify({ ...filters, ownerUserIds, maxAmount })
    if (pendingOperation.current?.signature !== signature) {
      pendingOperation.current = { signature, id: crypto.randomUUID() }
    }
    reclaim.mutate(
      {
        ...filters,
        ownerUserIds,
        maxAmount,
        operationId: pendingOperation.current.id,
      },
      {
        onSuccess: (result) => {
          pendingOperation.current = null
          toast.success(
            t('实际回收 {{count}} 条收益，共 {{amount}}', {
              count: result.reclaimed_count,
              amount: formatQuota(result.reclaimed_amount),
            })
          )
          setSelectedIDs([])
          setAmount('')
        },
        onError: (error) =>
          toast.error(
            error instanceof Error ? error.message : t('额度回收失败')
          ),
      }
    )
  }

  return (
    <section aria-label={t('渠道主收益管理')} className='my-4 space-y-2'>
      <h2 className='text-lg font-semibold'>{t('渠道主收益管理')}</h2>
      <p className='text-muted-foreground text-sm'>
        {t('按渠道主外部 ID 和收益时间筛选，勾选后回收已到账收益。')}
      </p>
      <AdminIncomeFilter
        report={query.data}
        ownerSearch={props.ownerSearch}
        onOwnerSearchChange={(value) => {
          setSelectedIDs([])
          props.onOwnerSearchChange(value)
        }}
        range={props.range}
        onRangeChange={(range) => {
          setSelectedIDs([])
          props.onRangeChange(range)
        }}
        onRefresh={() => void query.refetch()}
        isFetching={query.isFetching}
        isError={query.isError}
        releasing={reclaim.isPending}
        reclaimAmount={amount}
        onReclaimAmountChange={setAmount}
        selectedOwnerIDs={selected.map((item) => item.owner_user_id)}
        onSelectedOwnerIDsChange={setSelectedIDs}
        onRelease={submit}
      />
      <p className='text-muted-foreground text-xs'>
        {t(
          '金额单位与上方收益显示一致。留空回收全部；输入金额将按所选渠道主合计精确回收，优先扣除较早收益。任何一位渠道主额度不足时，本次操作全部取消。'
        )}
      </p>
    </section>
  )
}
