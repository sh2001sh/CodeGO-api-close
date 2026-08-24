import { useMemo } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { Eye, Radio, UserRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota, formatUseTime } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import type { MarketplaceOwnerUsageLog } from '@/features/marketplace/types'

export function useOwnerChannelLogColumns(
  onInspect: (item: MarketplaceOwnerUsageLog) => void
) {
  const { t } = useTranslation()
  return useMemo<ColumnDef<MarketplaceOwnerUsageLog>[]>(
    () => [
      {
        accessorKey: 'created_at',
        header: t('时间'),
        cell: ({ row }) => renderDateTime(row.original.created_at),
        meta: { label: t('时间') },
      },
      {
        id: 'channel_model',
        header: t('渠道 / 模型'),
        cell: ({ row }) => (
          <div className='min-w-44'>
            <div className='font-medium'>{row.original.channel_name}</div>
            <div className='text-muted-foreground mt-0.5 flex flex-wrap items-center gap-1.5 text-xs'>
              <span>{row.original.model_name || '-'}</span>
              {row.original.is_stream && (
                <Badge variant='outline' className='h-5 px-1.5 text-[10px]'>
                  <Radio className='size-2.5' />
                  {t('流式')}
                </Badge>
              )}
            </div>
            {row.original.request_id && (
              <div className='text-muted-foreground mt-1 max-w-56 truncate font-mono text-[10px]'>
                {row.original.request_id}
              </div>
            )}
          </div>
        ),
        meta: { label: t('渠道 / 模型'), mobileTitle: true },
      },
      {
        accessorKey: 'user_id',
        header: t('用户外部 ID'),
        cell: ({ row }) => (
          <span className='inline-flex items-center gap-1 font-mono text-xs'>
            <UserRound className='text-muted-foreground size-3' />
            {row.original.user_id || '-'}
          </span>
        ),
        meta: { label: t('用户外部 ID') },
      },
      {
        id: 'tokens',
        header: t('Tokens'),
        cell: ({ row }) => (
          <div className='text-xs tabular-nums'>
            <div>{row.original.prompt_tokens.toLocaleString()} 输入</div>
            <div className='text-muted-foreground mt-0.5'>
              {row.original.completion_tokens.toLocaleString()} 输出
            </div>
          </div>
        ),
        meta: { label: t('Tokens') },
      },
      {
        id: 'timing',
        header: t('首字 / 总耗时'),
        cell: ({ row }) => (
          <div className='text-xs tabular-nums'>
            <div>{formatMilliseconds(row.original.first_byte_ms)}</div>
            <div className='text-muted-foreground mt-0.5'>
              {formatMilliseconds(row.original.total_duration_ms)}
            </div>
          </div>
        ),
        meta: { label: t('首字 / 总耗时') },
      },
      {
        accessorKey: 'consumer_amount',
        header: t('用户扣费'),
        cell: ({ row }) => (
          <span className='font-mono text-xs font-medium tabular-nums'>
            {formatQuota(row.original.consumer_amount)}
          </span>
        ),
        meta: { label: t('用户扣费') },
      },
      {
        accessorKey: 'owner_income',
        header: t('渠道收入'),
        cell: ({ row }) => (
          <span className='text-success font-mono text-xs font-semibold tabular-nums'>
            +{formatQuota(row.original.owner_income)}
          </span>
        ),
        meta: { label: t('渠道收入') },
      },
      {
        id: 'status',
        header: t('状态'),
        cell: ({ row }) => renderIncomeStatus(row.original, t),
        meta: { label: t('状态'), mobileBadge: true },
      },
      {
        id: 'details',
        header: t('详情'),
        cell: ({ row }) => (
          <Button
            variant='ghost'
            size='icon'
            className='size-8'
            onClick={() => onInspect(row.original)}
            aria-label={t('查看调用详情')}
            title={t('查看调用详情')}
          >
            <Eye className='size-4' />
          </Button>
        ),
        meta: { label: t('详情') },
      },
    ],
    [onInspect, t]
  )
}

function formatMilliseconds(value: number) {
  return value > 0 ? formatUseTime(value / 1000) : '-'
}

function renderDateTime(timestamp: number) {
  const value = new Date(timestamp * 1000)
  return (
    <div className='text-xs tabular-nums'>
      <div>{value.toLocaleDateString()}</div>
      <div className='text-muted-foreground mt-0.5'>
        {value.toLocaleTimeString()}
      </div>
    </div>
  )
}

function renderIncomeStatus(
  item: MarketplaceOwnerUsageLog,
  t: (key: string) => string
) {
  if (item.status === 'failed') {
    return <Badge variant='destructive'>{t('调用失败')}</Badge>
  }
  if (item.income_status === 'released') {
    return <Badge className='bg-success/10 text-success'>{t('已到账')}</Badge>
  }
  if (item.income_status === 'pending') {
    return <Badge className='bg-warning/10 text-warning'>{t('待结算')}</Badge>
  }
  return <Badge variant='secondary'>{t('未入账')}</Badge>
}
