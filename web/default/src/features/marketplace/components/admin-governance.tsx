import { useDeferredValue, useState } from 'react'
import { Activity, Pencil, ShieldCheck, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useAdminMarketplaceChannels,
  useAdminOwnerIncome,
  useAdminMarketplaceVerification,
} from '../hooks'
import { MARKETPLACE_SOURCE_OPTIONS } from '../lib/channel-form'
import { hasGPT56Model } from '../lib/verification'
import type { MarketplaceChannel } from '../types'
import { AdminIncomeFilter, type AdminIncomeRange } from './admin-income-filter'
import { ChannelDeleteDialog } from './channel-delete-dialog'
import { ChannelEditDialog } from './channel-edit-dialog'
import { GPT56MappingStatusView } from './gpt56-mapping-report'
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
  const [mappingStatus, setMappingStatus] = useState('')
  const deferredOwnerSearch = useDeferredValue(ownerSearch.trim())
  const deferredChannelSearch = useDeferredValue(channelSearch.trim())
  const query = useAdminMarketplaceChannels(
    {
      search: deferredChannelSearch,
      status: channelStatus,
      source: channelSource,
      provider: channelProvider,
      verification: channelVerification,
      mappingStatus,
      ownerSearch: deferredOwnerSearch,
      startTimestamp: toTimestamp(incomeRange.start),
      endTimestamp: toTimestamp(incomeRange.end),
    },
    true
  )
  const ownerIncomeQuery = useAdminOwnerIncome({
    ownerSearch: deferredOwnerSearch,
    startTimestamp: toTimestamp(incomeRange.start),
    endTimestamp: toTimestamp(incomeRange.end),
  })
  const detection = useAdminMarketplaceVerification('detect')
  const connectivityTest = useAdminMarketplaceVerification('test')
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
      <AdminIncomeFilter
        report={ownerIncomeQuery.data}
        ownerSearch={ownerSearch}
        onOwnerSearchChange={setOwnerSearch}
        range={incomeRange}
        onRangeChange={setIncomeRange}
        onRefresh={() => {
          void query.refetch()
          void ownerIncomeQuery.refetch()
        }}
        isFetching={query.isFetching || ownerIncomeQuery.isFetching}
        isError={ownerIncomeQuery.isError}
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
          <option value='failed'>{t('检测未通过')}</option>
        </NativeSelect>
        <NativeSelect
          value={mappingStatus}
          onChange={(event) => setMappingStatus(event.target.value)}
          aria-label={t('映射状态')}
          className='bg-background'
        >
          <option value=''>{t('全部映射状态')}</option>
          <option value='matched'>{t('映射通过')}</option>
          <option value='insufficient_evidence'>{t('证据不足')}</option>
          <option value='mismatch'>{t('映射不一致')}</option>
        </NativeSelect>
      </div>
      <section className='border-border overflow-hidden rounded-md border'>
        {query.isLoading ? (
          <div className='space-y-2 p-3'>
            {Array.from({ length: 5 }).map((_, index) => (
              <Skeleton key={index} className='h-24 w-full' />
            ))}
          </div>
        ) : (query.data ?? []).length === 0 ? (
          <div className='px-4 py-12 text-center text-sm'>
            {t('当前没有待治理渠道')}
          </div>
        ) : (
          <div className='divide-border divide-y'>
            {(query.data ?? []).map((channel) => (
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
                      {t('已释放')}: {formatQuota(channel.released_income)}
                    </span>
                    <span>
                      {t('结算请求')}: {channel.request_count.toLocaleString()}
                    </span>
                  </div>
                  <SensitiveWordPolicyControl channel={channel} admin />
                  <GPT56MappingStatusView
                    models={channel.declared_models}
                    status={channel.gpt56_mapping_status}
                    results={channel.gpt56_mapping_results}
                    checkedAt={channel.gpt56_mapping_checked_at}
                    level={channel.gpt56_mapping_level}
                    trigger={channel.gpt56_mapping_trigger}
                    history={channel.gpt56_mapping_history}
                  />
                  <ConnectivityTestStatusView
                    status={channel.connectivity_test_status}
                    results={channel.model_verification_results}
                    checkedAt={channel.connectivity_test_checked_at}
                    required={!hasGPT56Model(channel.declared_models)}
                    showErrors
                  />
                </div>
                <div className='flex shrink-0 flex-wrap items-center gap-2'>
                  {hasGPT56Model(channel.declared_models) && (
                    <Button
                      variant='outline'
                      size='sm'
                      disabled={
                        detection.isPending ||
                        channel.gpt56_mapping_status === 'running'
                      }
                      onClick={() => detection.mutate(channel.id)}
                    >
                      <ShieldCheck />
                      {channel.gpt56_mapping_status === 'running'
                        ? t('检测中')
                        : t('检测 GPT-5.6')}
                    </Button>
                  )}
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={
                      connectivityTest.isPending ||
                      ['queued', 'running'].includes(
                        channel.connectivity_test_status
                      )
                    }
                    onClick={() => connectivityTest.mutate(channel.id)}
                  >
                    <Activity />
                    {['queued', 'running'].includes(
                      channel.connectivity_test_status
                    )
                      ? t('测试中')
                      : t('测试连通性')}
                  </Button>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => setEditing(channel)}
                  >
                    <Pencil />
                    {t('编辑渠道')}
                  </Button>
                  <Button
                    variant='ghost'
                    size='sm'
                    className='text-destructive hover:text-destructive'
                    onClick={() => setDeleting(channel)}
                  >
                    <Trash2 />
                    {t('删除')}
                  </Button>
                </div>
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

function toTimestamp(value?: Date) {
  return value ? Math.floor(value.getTime() / 1000) : undefined
}
