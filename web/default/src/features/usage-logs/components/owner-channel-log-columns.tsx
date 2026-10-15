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
              <div
                title={row.original.request_id}
                className='text-muted-foreground mt-1 max-w-56 truncate font-mono text-[10px]'
              >
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
            <div>
              {row.original.prompt_tokens.toLocaleString()} {t('输入')}
            </div>
            <div className='text-success mt-0.5'>
              {t('缓存命中')}:{' '}
              {(row.original.cache_read_tokens ?? 0).toLocaleString()}
            </div>
            {(row.original.cache_write_tokens ?? 0) > 0 && (
              <div className='text-muted-foreground mt-0.5'>
                {t('缓存写入')}:{' '}
                {row.original.cache_write_tokens.toLocaleString()}
              </div>
            )}
            <div className='text-muted-foreground mt-0.5'>
              {row.original.completion_tokens.toLocaleString()} {t('输出')}
            </div>
          </div>
        ),
        meta: { label: t('Tokens') },
      },
      {
        id: 'timing',
        header: t('First output') + ' / ' + t('总耗时'),
        cell: ({ row }) => (
          <div className='text-xs tabular-nums'>
            <div>{formatMilliseconds(row.original.first_byte_ms)}</div>
            <div className='text-muted-foreground mt-0.5'>
              {formatMilliseconds(row.original.total_duration_ms)}
            </div>
          </div>
        ),
        meta: { label: t('First output') + ' / ' + t('总耗时') },
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
        header: t('调用 / 结算'),
        cell: ({ row }) => (
          <div className='space-y-1'>
            {row.original.status !== 'failed' && (
              <div className='text-success text-xs'>{t('调用成功')}</div>
            )}
            {renderIncomeStatus(row.original, t)}
            {row.original.reclaimed_income > 0 && (
              <p className='text-muted-foreground text-xs'>
                {t('已回收')}: {formatQuota(row.original.reclaimed_income)}
              </p>
            )}
            {row.original.status === 'failed' && (
              <p
                className='text-destructive max-w-52 truncate text-xs'
                title={row.original.error_message}
              >
                {row.original.status_code > 0 &&
                  `HTTP ${row.original.status_code} · `}
                {row.original.error_message || t('查看详情了解失败原因')}
              </p>
            )}
          </div>
        ),
        meta: { label: t('调用 / 结算'), mobileBadge: true },
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
    if (item.reclaimed_income > 0) {
      return <Badge variant='secondary'>{t('部分回收')}</Badge>
    }
    return <Badge className='bg-success/10 text-success'>{t('已到账')}</Badge>
  }
  if (item.income_status === 'reclaimed') {
    return <Badge variant='secondary'>{t('已回收')}</Badge>
  }
  if (item.income_status === 'pending') {
    return <Badge className='bg-warning/10 text-warning'>{t('待结算')}</Badge>
  }
  return <Badge variant='secondary'>{t('未入账')}</Badge>
}
