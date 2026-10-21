import { useEffect, useState } from 'react'
import interfaceI18n from '@/i18n/config'
import { getIntlLocale } from '@/i18n/format'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { AdminTableStatus } from '@/features/channels/components/admin-table-status'
import {
  useAdminMarketplaceChannels,
  useMarketplaceFailedModelRemoval,
} from '../hooks'
import { MARKETPLACE_SOURCE_OPTIONS } from '../lib/channel-form'
import { localizedSourceLabel } from '../lib/localized-group-name'
import type { MarketplaceChannel } from '../types'
import { AdminChannelActions } from './admin-channel-actions'
import type { AdminIncomeRange } from './admin-income-filter'
import { AdminOwnerIncomePanel } from './admin-owner-income-panel'
import { ChannelDeleteDialog } from './channel-delete-dialog'
import { ChannelEditDialog } from './channel-edit-dialog'
import { MarketReportsPanel } from './market-reports-panel'
import {
  ConnectivityTestStatusView,
  ModelConsistencyBadge,
} from './model-verification'
import { SensitiveWordPolicyControl } from './sensitive-word-policy-control'
import { MarketplaceStatusBadge } from './status-badge'

export function AdminGovernance() {
  const { t, i18n } = useTranslation()
  const [section, setSection] = useState('channels')
  const [incomeRange, setIncomeRange] = useState<AdminIncomeRange>({})
  const [ownerSearch, setOwnerSearch] = useState('')
  const [channelSearch, setChannelSearch] = useState('')
  const [channelStatus, setChannelStatus] = useState('')
  const [channelSource, setChannelSource] = useState('')
  const [channelProvider, setChannelProvider] = useState('')
  const [channelVerification, setChannelVerification] = useState('')
  const deferredOwnerSearch = useDebouncedValue(ownerSearch.trim(), 350)
  const deferredChannelSearch = useDebouncedValue(channelSearch.trim(), 350)
  const filters = {
    search: deferredChannelSearch,
    status: channelStatus,
    source: channelSource,
    provider: channelProvider,
    verification: channelVerification,
    ownerSearch: deferredOwnerSearch,
    startTimestamp: toTimestamp(incomeRange.start),
    endTimestamp: toTimestamp(incomeRange.end),
  }
  const filterKey = JSON.stringify(filters)
  const [position, setPosition] = useState({ filterKey: '', page: 1 })
  const page = position.filterKey === filterKey ? position.page : 1
  const query = useAdminMarketplaceChannels(
    { ...filters, page, pageSize: 50 },
    section === 'channels'
  )
  const failedModelRemoval = useMarketplaceFailedModelRemoval(true)
  const [editing, setEditing] = useState<MarketplaceChannel | null>(null)
  const [deleting, setDeleting] = useState<MarketplaceChannel | null>(null)
  const pageCount = Math.max(1, Math.ceil((query.data?.total ?? 0) / 50))
  if (query.data && !query.isPlaceholderData && page > pageCount) {
    setPosition({ filterKey, page: pageCount })
  }

  return (
    <Tabs
      value={section}
      onValueChange={setSection}
      className='marketplace-governance gap-5'
    >
      <TabsList aria-label={t('市场分组治理')} className='w-full'>
        <TabsTrigger value='channels'>{t('渠道')}</TabsTrigger>
        <TabsTrigger value='reports'>{t('举报')}</TabsTrigger>
        <TabsTrigger value='income'>{t('渠道主收益')}</TabsTrigger>
      </TabsList>
      <TabsContent value='reports'>
        <MarketReportsPanel />
      </TabsContent>
      <TabsContent value='income'>
        <AdminOwnerIncomePanel
          ownerSearch={ownerSearch}
          onOwnerSearchChange={setOwnerSearch}
          range={incomeRange}
          onRangeChange={setIncomeRange}
        />
      </TabsContent>
      <TabsContent value='channels' className='space-y-4'>
        <AdminTableStatus
          title={t('市场渠道')}
          total={query.data?.total}
          loading={query.isLoading}
          fetching={query.isFetching}
          onRetry={() => void query.refetch()}
        />
        <div className='governance-filter-bar'>
          <Input
            value={channelSearch}
            onChange={(event) => setChannelSearch(event.target.value)}
            placeholder={t('搜索分组、渠道 ID、模型或来源')}
            aria-label={t('搜索分组、渠道 ID、模型或来源')}
            className='governance-search bg-background min-w-0 flex-1'
          />
          <Input
            value={ownerSearch}
            onChange={(event) => setOwnerSearch(event.target.value)}
            placeholder={t('渠道主 ID')}
            aria-label={t('渠道主 ID')}
            className='w-full sm:w-44'
          />
          <Input
            value={channelSource}
            onChange={(event) => setChannelSource(event.target.value)}
            aria-label={t('来源')}
            placeholder={t('全部来源')}
            list='admin-market-source-suggestions'
            className='bg-background w-full sm:w-44'
          />
          <datalist id='admin-market-source-suggestions'>
            {MARKETPLACE_SOURCE_OPTIONS.map((source) => (
              <option key={source} value={source}>
                {localizedSourceLabel(source, i18n.language)}
              </option>
            ))}
          </datalist>
          <NativeSelect
            value={channelProvider}
            onChange={(event) => setChannelProvider(event.target.value)}
            aria-label={t('协议类型')}
            className='bg-background'
          >
            <option value=''>{t('全部协议')}</option>
            <option value='openai_compatible'>
              {interfaceI18n.t('OpenAI Compatible')}
            </option>
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
        <section className='admin-ledger-panel' aria-busy={query.isFetching}>
          {query.isLoading || query.isPlaceholderData ? (
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
                  className='admin-governance-record flex flex-col gap-5 px-5 py-6 lg:flex-row lg:items-start lg:justify-between'
                >
                  <div className='min-w-0'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <span className='font-medium'>
                        {channel.custom_name
                          ? `${channel.id}-${channel.custom_name}`
                          : channel.system_display_name}
                      </span>
                      <MarketplaceStatusBadge
                        status={channel.lifecycle_status}
                      />
                      <ModelConsistencyBadge
                        status={channel.model_consistency_status}
                      />
                    </div>
                    <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs'>
                      <span className='font-mono tabular-nums'>
                        ID {channel.id}
                      </span>
                      <span className='text-foreground font-medium tabular-nums'>
                        {t('渠道主 ID')}: {channel.owner_external_id || '--'}
                      </span>
                      <span>{channel.provider_type}</span>
                      <span className='text-foreground font-medium'>
                        {t('渠道主声明')}:{' '}
                        {channel.submitted_source_label || '--'}
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
                        {t('已到账收益')}:{' '}
                        {formatQuota(channel.released_income)}
                      </span>
                      <span>
                        {t('已回收额度')}:{' '}
                        {formatQuota(channel.reclaimed_income)}
                      </span>
                      <span>
                        {t('结算请求')}:{' '}
                        {channel.request_count.toLocaleString(getIntlLocale())}
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
        {query.data && !query.isError && (
          <nav
            className='flex flex-wrap items-center justify-between gap-3 text-sm'
            aria-label={t('分页')}
          >
            <span>
              {t('Page {{current}} of {{total}}', {
                current: page,
                total: pageCount,
              })}
            </span>
            <div className='flex gap-2'>
              <Button
                variant='outline'
                disabled={query.isFetching || page <= 1}
                onClick={() => setPosition({ filterKey, page: page - 1 })}
              >
                {t('上一页')}
              </Button>
              <Button
                variant='outline'
                disabled={query.isFetching || page >= pageCount}
                onClick={() => setPosition({ filterKey, page: page + 1 })}
              >
                {t('下一页')}
              </Button>
            </div>
          </nav>
        )}
      </TabsContent>
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
    </Tabs>
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
