import { useEffect, useState } from 'react'
import { ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useAdminMarketplaceChannels,
  useMarketplaceFailedModelRemoval,
} from '../hooks'
import { MARKETPLACE_SOURCE_OPTIONS } from '../lib/channel-form'
import type { MarketplaceChannel } from '../types'
import { AdminChannelActions } from './admin-channel-actions'
import type { AdminIncomeRange } from './admin-income-filter'
import { AdminOwnerIncomePanel } from './admin-owner-income-panel'
import { ChannelDeleteDialog } from './channel-delete-dialog'
import { ChannelEditDialog } from './channel-edit-dialog'
import {
  ConnectivityTestStatusView,
  ModelConsistencyBadge,
} from './model-verification'
import { SensitiveWordPolicyControl } from './sensitive-word-policy-control'
import { MarketplaceStatusBadge } from './status-badge'

export function AdminGovernance() {
  const { t } = useTranslation()
  const [incomeRange, setIncomeRange] = useState<AdminIncomeRange>({})
  const [ownerSearch, setOwnerSearch] = useState('')
  const [channelSearch, setChannelSearch] = useState('')
  const [channelStatus, setChannelStatus] = useState('')
  const [channelSource, setChannelSource] = useState('')
  const [channelProvider, setChannelProvider] = useState('')
  const [channelVerification, setChannelVerification] = useState('')
  const deferredOwnerSearch = useDebouncedValue(ownerSearch.trim(), 350)
  const deferredChannelSearch = useDebouncedValue(channelSearch.trim(), 350)
  const query = useAdminMarketplaceChannels(
    {
      search: deferredChannelSearch,
      status: channelStatus,
      source: channelSource,
      provider: channelProvider,
      verification: channelVerification,
      ownerSearch: deferredOwnerSearch,
      startTimestamp: toTimestamp(incomeRange.start),
      endTimestamp: toTimestamp(incomeRange.end),
    },
    true
  )
  const failedModelRemoval = useMarketplaceFailedModelRemoval(true)
  const [editing, setEditing] = useState<MarketplaceChannel | null>(null)
  const [deleting, setDeleting] = useState<MarketplaceChannel | null>(null)

  return (
    <div className='space-y-4'>
      <div className='border-success/35 bg-success/8 flex items-start gap-3 rounded-md border p-3'>
        <ShieldCheck className='text-success mt-0.5 size-4' />
        <p className='text-sm leading-6'>
          {t(
            '固定来源不再需要人工审核。管理员可直接编辑协议、来源、模型、连接和服务策略；连接或模型变更会重新检测。'
          )}
        </p>
      </div>
      <AdminOwnerIncomePanel
        ownerSearch={ownerSearch}
        onOwnerSearchChange={setOwnerSearch}
        range={incomeRange}
        onRangeChange={setIncomeRange}
      />
      <div className='border-border bg-muted/10 flex flex-wrap items-center gap-2 rounded-md border p-3'>
        <Input
          value={channelSearch}
          onChange={(event) => setChannelSearch(event.target.value)}
          placeholder={t('搜索分组、渠道 ID、模型或来源')}
          aria-label={t('搜索分组、渠道 ID、模型或来源')}
          className='bg-background min-w-64 flex-1'
        />
        <NativeSelect
          value={channelSource}
          onChange={(event) => setChannelSource(event.target.value)}
          aria-label={t('来源')}
          className='bg-background'
        >
          <option value=''>{t('全部来源')}</option>
          {MARKETPLACE_SOURCE_OPTIONS.map((source) => (
            <option key={source} value={source}>
              {source}
            </option>
          ))}
        </NativeSelect>
        <NativeSelect
          value={channelProvider}
          onChange={(event) => setChannelProvider(event.target.value)}
          aria-label={t('协议类型')}
          className='bg-background'
        >
          <option value=''>{t('全部协议')}</option>
          <option value='openai_compatible'>OpenAI Compatible</option>
          <option value='codex'>Codex</option>
          <option value='azure_openai'>Azure OpenAI</option>
          <option value='anthropic'>Anthropic / Claude</option>
          <option value='gemini'>Google Gemini</option>
        </NativeSelect>
        <NativeSelect
          value={channelStatus}
          onChange={(event) => setChannelStatus(event.target.value)}
          aria-label={t('状态')}
          className='bg-background'
        >
          <option value=''>{t('全部状态')}</option>
          <option value='draft'>{t('草稿')}</option>
          <option value='verifying'>{t('检测中')}</option>
          <option value='pending_review'>{t('待审核')}</option>
          <option value='active'>{t('可用')}</option>
          <option value='degraded'>{t('质量下降')}</option>
          <option value='suspended'>{t('已暂停')}</option>
          <option value='disabled'>{t('已停用')}</option>
        </NativeSelect>
        <NativeSelect
          value={channelVerification}
          onChange={(event) => setChannelVerification(event.target.value)}
          aria-label={t('检测状态')}
          className='bg-background'
        >
          <option value=''>{t('全部检测状态')}</option>
          <option value='passed'>{t('检测通过')}</option>
          <option value='queued'>{t('等待检测')}</option>
          <option value='running'>{t('检测中')}</option>
          <option value='paused'>{t('已暂停')}</option>
          <option value='failed'>{t('检测未通过')}</option>
        </NativeSelect>
      </div>
      <section className='border-border overflow-hidden rounded-md border'>
        {query.isLoading ? (
          <div className='space-y-2 p-3'>
            {Array.from({ length: 5 }).map((_, index) => (
              <Skeleton key={index} className='h-24 w-full' />
            ))}
          </div>
        ) : query.isError ? (
          <div role='alert' className='text-destructive px-4 py-8 text-sm'>
            {t('渠道数据加载失败')}
            <Button
              variant='outline'
              size='sm'
              onClick={() => void query.refetch()}
            >
              {t('重试')}
            </Button>
          </div>
        ) : (query.data?.items ?? []).length === 0 ? (
          <div className='px-4 py-12 text-center text-sm'>
            {t('当前没有待治理渠道')}
          </div>
        ) : (
          <div className='divide-border divide-y'>
            {(query.data?.items ?? []).map((channel) => (
              <div
                key={channel.id}
                className='flex flex-col gap-3 px-4 py-4 sm:flex-row sm:items-center sm:justify-between'
              >
                <div className='min-w-0'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='font-medium'>
                      {channel.system_display_name}
                    </span>
                    <MarketplaceStatusBadge status={channel.lifecycle_status} />
                    <ModelConsistencyBadge
                      status={channel.model_consistency_status}
                    />
                  </div>
                  <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs'>
                    <span className='tabular-nums'>ID {channel.id}</span>
                    <span className='text-foreground font-medium tabular-nums'>
                      {t('渠道主 ID')}: {channel.owner_external_id || '--'}
                    </span>
                    <span>{channel.provider_type}</span>
                    <span className='text-foreground font-medium'>
                      {t('来源')}: {channel.submitted_source_label || '--'}
                    </span>
                    <span>
                      {channel.declared_models.length} {t('个模型')}
                    </span>
                    <span>{channel.multiplier.toFixed(2)}x</span>
                    <span>
                      {t('检测')}: {channel.verification_status}
                    </span>
                    <span>
                      {t('总并发')} {channel.max_concurrency || t('不限')}
                    </span>
                    <span>
                      {t('单用户并发')}{' '}
                      {channel.user_max_concurrency || t('不限')}
                    </span>
                    <span>QPS {channel.qps}</span>
                  </div>
                  <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs'>
                    <span>
                      {t('筛选收益')}:{' '}
                      <strong className='text-foreground tabular-nums'>
                        {formatQuota(channel.total_income)}
                      </strong>
                    </span>
                    <span>
                      {t('待结算')}: {formatQuota(channel.pending_income)}
                    </span>
                    <span>
                      {t('已到账收益')}: {formatQuota(channel.released_income)}
                    </span>
                    <span>
                      {t('已回收额度')}: {formatQuota(channel.reclaimed_income)}
                    </span>
                    <span>
                      {t('结算请求')}: {channel.request_count.toLocaleString()}
                    </span>
                  </div>
                  <SensitiveWordPolicyControl channel={channel} admin />
                  <ConnectivityTestStatusView
                    status={channel.connectivity_test_status}
                    results={channel.model_verification_results}
                    checkedAt={channel.connectivity_test_checked_at}
                    summary={channel.verification_summary}
                    required
                    showErrors
                    onRemoveModel={(model) =>
                      failedModelRemoval.mutate(
                        { channelId: channel.id, model },
                        {
                          onSuccess: () =>
                            toast.success(
                              t('已剔除失败模型 {{model}}', { model })
                            ),
                          onError: (error) =>
                            toast.error(
                              error instanceof Error
                                ? error.message
                                : t('剔除模型失败')
                            ),
                        }
                      )
                    }
                    removingModel={
                      failedModelRemoval.isPending &&
                      failedModelRemoval.variables?.channelId === channel.id
                        ? failedModelRemoval.variables.model
                        : undefined
                    }
                  />
                </div>
                <AdminChannelActions
                  channel={channel}
                  onEdit={() => setEditing(channel)}
                  onDelete={() => setDeleting(channel)}
                />
              </div>
            ))}
          </div>
        )}
      </section>
      <ChannelEditDialog
        admin
        channel={editing}
        open={editing != null}
        onOpenChange={(open) => {
          if (!open) setEditing(null)
        }}
      />
      <ChannelDeleteDialog
        admin
        channel={deleting}
        open={deleting != null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null)
        }}
      />
    </div>
  )
}

function useDebouncedValue<T>(value: T, delay: number) {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay)
    return () => window.clearTimeout(timer)
  }, [delay, value])
  return debounced
}

function toTimestamp(value?: Date) {
  return value ? Math.floor(value.getTime() / 1000) : undefined
}
