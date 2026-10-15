import { AlertTriangle, Clock3, Copy, Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import type { MarketplaceOwnerUsageLog } from '@/features/marketplace/types'

export function OwnerChannelLogDetailsDialog(props: {
  item: MarketplaceOwnerUsageLog | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const item = props.item
  const { copyToClipboard } = useCopyToClipboard({ notify: true })
  return (
    <Dialog open={Boolean(item)} onOpenChange={props.onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('渠道调用详情')}</DialogTitle>
          <DialogDescription>
            {t('查看请求标识、首字分段、总耗时与上游返回。')}
          </DialogDescription>
        </DialogHeader>
        {item && (
          <ScrollArea className='max-h-[70dvh] pr-3'>
            <div className='space-y-5 py-2'>
              <DetailSection icon={<Route />} title={t('请求与路由')}>
                <DetailRow label={t('渠道')} value={item.channel_name} />
                <DetailRow
                  label={t('模型')}
                  value={item.model_name || '-'}
                  mono
                />
                <DetailRow
                  label={t('用户外部 ID')}
                  value={item.user_id || '-'}
                  mono
                />
                <CopyableRow
                  label={t('请求 ID')}
                  value={item.request_id}
                  onCopy={copyToClipboard}
                />
                <CopyableRow
                  label={t('上游请求 ID')}
                  value={item.upstream_request_id}
                  onCopy={copyToClipboard}
                />
                <DetailRow
                  label={t('请求路径')}
                  value={item.request_path || '-'}
                  mono
                />
                <DetailRow
                  label={t('重试次数')}
                  value={String(item.retry_count)}
                />
              </DetailSection>
              <DetailSection icon={<Route />} title={t('收益与回收')}>
                <DetailRow
                  label={t('原始渠道收益')}
                  value={formatQuota(item.owner_income)}
                />
                <DetailRow
                  label={t('已回收额度')}
                  value={formatQuota(item.reclaimed_income || 0)}
                />
                {item.income_status === 'released' && (
                  <DetailRow
                    label={t('剩余已到账收益')}
                    value={formatQuota(
                      item.owner_income - (item.reclaimed_income || 0)
                    )}
                  />
                )}
              </DetailSection>
              <DetailSection icon={<Route />} title={t('Token 与缓存')}>
                <DetailRow
                  label={t('输入 Token')}
                  value={item.prompt_tokens.toLocaleString()}
                />
                <DetailRow
                  label={t('缓存命中 Token')}
                  value={(item.cache_read_tokens ?? 0).toLocaleString()}
                />
                <DetailRow
                  label={t('缓存写入 Token')}
                  value={(item.cache_write_tokens ?? 0).toLocaleString()}
                />
                <DetailRow
                  label={t('输出 Token')}
                  value={item.completion_tokens.toLocaleString()}
                />
              </DetailSection>
              <DetailSection icon={<Clock3 />} title={t('耗时')}>
                <DetailRow
                  label={t('First output') + ' · ' + t('Attempt')}
                  value={formatMs(item.attempt_ttft_ms)}
                />
                <DetailRow
                  label={t('First output')}
                  value={formatMs(item.first_byte_ms)}
                />
                <DetailRow
                  label={t('总耗时')}
                  value={formatMs(item.total_duration_ms)}
                />
                {item.first_byte_trace && (
                  <pre className='bg-background overflow-x-auto rounded-md border p-2.5 text-[11px] leading-5'>
                    {JSON.stringify(item.first_byte_trace, null, 2)}
                  </pre>
                )}
              </DetailSection>
              {item.status === 'failed' && (
                <DetailSection
                  icon={<AlertTriangle />}
                  title={t('真实上游错误')}
                  danger
                >
                  <DetailRow
                    label={t('HTTP 状态')}
                    value={item.status_code ? String(item.status_code) : '-'}
                  />
                  <DetailRow
                    label={t('错误类型')}
                    value={item.error_type || '-'}
                    mono
                  />
                  <DetailRow
                    label={t('错误代码')}
                    value={item.error_code || '-'}
                    mono
                  />
                  <div className='border-destructive/30 bg-destructive/5 relative rounded-md border p-3 pr-11'>
                    <p className='text-destructive text-xs leading-5 break-all whitespace-pre-wrap'>
                      {item.error_message ||
                        t('旧日志未保存渠道主可见的原始错误。')}
                    </p>
                    {item.error_message && (
                      <Button
                        variant='ghost'
                        size='icon'
                        className='absolute top-1.5 right-1.5 size-8'
                        onClick={() => copyToClipboard(item.error_message)}
                        aria-label={t('复制错误')}
                      >
                        <Copy className='size-3.5' />
                      </Button>
                    )}
                  </div>
                </DetailSection>
              )}
            </div>
          </ScrollArea>
        )}
      </DialogContent>
    </Dialog>
  )
}

function DetailSection(props: {
  icon: React.ReactNode
  title: string
  danger?: boolean
  children: React.ReactNode
}) {
  return (
    <section>
      <h3
        className={
          props.danger
            ? 'text-destructive flex items-center gap-2 text-sm font-semibold'
            : 'flex items-center gap-2 text-sm font-semibold'
        }
      >
        <span className='[&>svg]:size-4'>{props.icon}</span>
        {props.title}
      </h3>
      <div className='border-border mt-2 space-y-2 border-t pt-3'>
        {props.children}
      </div>
    </section>
  )
}

function DetailRow(props: { label: string; value: string; mono?: boolean }) {
  return (
    <div className='grid grid-cols-[7rem_minmax(0,1fr)] gap-3 text-xs'>
      <span className='text-muted-foreground'>{props.label}</span>
      <span className={props.mono ? 'font-mono break-all' : 'break-words'}>
        {props.value}
      </span>
    </div>
  )
}

function CopyableRow(props: {
  label: string
  value: string
  onCopy: (value: string) => void
}) {
  if (!props.value) return <DetailRow label={props.label} value='-' />
  return (
    <div className='grid grid-cols-[7rem_minmax(0,1fr)] gap-3 text-xs'>
      <span className='text-muted-foreground'>{props.label}</span>
      <button
        className='hover:text-primary focus-visible:ring-ring min-w-0 cursor-pointer text-left font-mono break-all focus-visible:ring-2 focus-visible:outline-none'
        onClick={() => props.onCopy(props.value)}
      >
        {props.value}
      </button>
    </div>
  )
}

function formatMs(value: number) {
  if (!value) return '-'
  return value < 1000
    ? `${Math.round(value)} ms`
    : `${(value / 1000).toFixed(2)} s`
}
