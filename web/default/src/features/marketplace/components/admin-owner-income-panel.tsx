import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import {
  useAdminOwnerIncome,
  useAdminOwnerIncomeReclaim,
  useAdminOwnerIncomeRelease,
} from '../hooks'
import { AdminIncomeFilter, type AdminIncomeRange } from './admin-income-filter'

const RECLAIM_OPERATION_STORAGE_KEY = 'admin-owner-income-reclaim-operation'

export function AdminOwnerIncomePanel(props: {
  ownerSearch: string
  onOwnerSearchChange: (value: string) => void
  range: AdminIncomeRange
  onRangeChange: (range: AdminIncomeRange) => void
}) {
  const { t } = useTranslation()
  const [selectedIDs, setSelectedIDs] = useState<number[]>([])
  const [amount, setAmount] = useState('')
  const [runningElapsedSeconds, setRunningElapsedSeconds] = useState(0)
  const [reclaimOperationID, setReclaimOperationID] = useState<
    string | undefined
  >(() =>
    typeof window === 'undefined'
      ? undefined
      : sessionStorage.getItem(RECLAIM_OPERATION_STORAGE_KEY) || undefined
  )
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
  const refetchIncome = query.refetch
  const reclaim = useAdminOwnerIncomeRelease()
  const reclaimTask = useAdminOwnerIncomeReclaim(reclaimOperationID)
  const completedOperation = useRef<string | undefined>(undefined)
  const selected = (query.data?.items ?? []).filter((item) =>
    selectedIDs.includes(item.owner_user_id)
  )
  const available = selected.reduce(
    (sum, item) => sum + item.reclaimable_quota,
    0
  )
  const taskElapsedSeconds =
    reclaimTask.data &&
    ['completed', 'failed'].includes(reclaimTask.data.status)
      ? Math.max(
          0,
          Math.floor(
            (new Date(reclaimTask.data.updated_at).getTime() -
              new Date(reclaimTask.data.created_at).getTime()) /
              1000
          )
        )
      : runningElapsedSeconds

  useEffect(() => {
    const task = reclaimTask.data
    if (!task || !['pending', 'running'].includes(task.status)) return
    const timer = window.setInterval(() => {
      setRunningElapsedSeconds(
        Math.max(
          0,
          Math.floor((Date.now() - new Date(task.created_at).getTime()) / 1000)
        )
      )
    }, 1000)
    return () => window.clearInterval(timer)
  }, [reclaimTask.data])

  useEffect(() => {
    const task = reclaimTask.data
    if (!task || completedOperation.current === task.operation_id) return
    if (task.status === 'completed') {
      completedOperation.current = task.operation_id
      pendingOperation.current = null
      void refetchIncome()
      toast.success(
        t('实际回收 {{count}} 条收益，共 {{amount}}', {
          count: task.reclaimed_count,
          amount: formatQuota(task.reclaimed_amount),
        })
      )
    }
    if (task.status === 'failed') {
      completedOperation.current = task.operation_id
      toast.error(task.error_message || t('额度回收失败'))
    }
  }, [reclaimTask.data, refetchIncome, t])

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
      : available
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
    if (maxAmount > available) {
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
          completedOperation.current = undefined
          setRunningElapsedSeconds(0)
          setReclaimOperationID(result.operation_id)
          sessionStorage.setItem(
            RECLAIM_OPERATION_STORAGE_KEY,
            result.operation_id
          )
          if (result.status === 'pending' || result.status === 'running') {
            toast.success(t('回收任务已创建，正在后台处理'))
          }
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
          '金额单位与上方收益显示一致。留空回收当前可回收额度；输入金额将按所选渠道主合计精确回收，优先扣除较早收益。任何一位渠道主额度不足时，本次操作全部取消。'
        )}
      </p>
      {reclaimTask.data && (
        <div
          className='border-border bg-muted/30 space-y-3 border p-4'
          aria-live='polite'
        >
          <div className='flex items-start justify-between gap-3'>
            <div>
              <p className='font-medium'>
                {reclaimTask.data.status === 'completed'
                  ? t('回收完成')
                  : reclaimTask.data.status === 'failed'
                    ? t('回收失败')
                    : t('正在回收')}
              </p>
              <p className='text-muted-foreground mt-1 text-xs'>
                {t('已处理 {{count}} 条 · 已提交 {{batches}} 批', {
                  count: reclaimTask.data.reclaimed_count,
                  batches: reclaimTask.data.batch_number,
                })}
              </p>
            </div>
            {!['pending', 'running'].includes(reclaimTask.data.status) && (
              <Button
                type='button'
                variant='ghost'
                size='sm'
                onClick={() => {
                  sessionStorage.removeItem(RECLAIM_OPERATION_STORAGE_KEY)
                  setReclaimOperationID(undefined)
                }}
              >
                {t('关闭结果')}
              </Button>
            )}
          </div>
          <Progress
            value={
              reclaimTask.data.target_amount > 0
                ? Math.min(
                    100,
                    (reclaimTask.data.reclaimed_amount /
                      reclaimTask.data.target_amount) *
                      100
                  )
                : null
            }
          />
          <div className='text-muted-foreground grid gap-1 text-xs sm:grid-cols-4'>
            <span>
              {t('已回收：{{amount}}', {
                amount: formatQuota(reclaimTask.data.reclaimed_amount),
              })}
            </span>
            <span>
              {t('目标：{{amount}}', {
                amount: formatQuota(reclaimTask.data.target_amount),
              })}
            </span>
            <span>
              {t('最近更新：{{time}}', {
                time: new Date(
                  reclaimTask.data.updated_at
                ).toLocaleTimeString(),
              })}
            </span>
            <span>
              {t('已运行：{{minutes}}分 {{seconds}}秒', {
                minutes: Math.floor(taskElapsedSeconds / 60),
                seconds: taskElapsedSeconds % 60,
              })}
            </span>
          </div>
          {reclaimTask.data.error_message && (
            <p className='text-destructive text-xs'>
              {reclaimTask.data.error_message}
            </p>
          )}
        </div>
      )}
    </section>
  )
}
