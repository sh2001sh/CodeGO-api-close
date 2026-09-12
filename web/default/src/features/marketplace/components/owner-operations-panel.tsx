import { useMemo, useState } from 'react'
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
  type UseQueryResult,
} from '@tanstack/react-query'
import { Check, RefreshCw, RotateCcw, Search, Send, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import dayjs from '@/lib/dayjs'
import { formatQuota } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  createMarketplaceTimeRangeMultiplier,
  deleteMarketplaceTimeRangeMultiplier,
  getMarketplaceTimeRangeMultipliers,
  getMyMarketplaceBargainRequests,
  getMyMarketplaceUserUsage,
  getOwnerMultipliers,
  batchSetMarketplaceUserMultipliers,
  resolveMarketplaceBargainRequest,
  sendMarketplaceBatchWelfare,
  type MarketplaceBatchWelfareResult,
} from '../api'
import { useMyMarketplaceChannels } from '../hooks'
import { rankOwnerUsers, type OwnerUsageSort } from '../lib/owner-usage-ranking'
import type {
  MarketplaceBargainRequestList,
  MarketplaceOwnerUsageItem,
} from '../types'

type WelfareType = 'transfer' | 'blind_box'
type DateRange = { start?: Date; end?: Date }

export function OwnerOperationsPanel({
  focus,
}: {
  focus?: { channelId: string; userId: number; nonce: number }
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const channels = useMyMarketplaceChannels()
  const manageableChannels = (channels.data ?? []).filter(
    (channel) => !channel.deleted_at
  )
  const [range, setRange] = useState<DateRange>(() => ({
    start: dayjs().startOf('day').toDate(),
    end: dayjs().endOf('day').toDate(),
  }))
  const initialUserSearch = focus?.userId ? String(focus.userId) : ''
  const [searchDraft, setSearchDraft] = useState(initialUserSearch)
  const [search, setSearch] = useState(initialUserSearch)
  const [channelID, setChannelID] = useState(focus?.channelId ?? '')
  const [rankingSort, setRankingSort] = useState<OwnerUsageSort>('requests')
  const activeChannelID = manageableChannels.some(
    (channel) => channel.id === channelID
  )
    ? channelID
    : manageableChannels[0]?.id || ''
  const usage = useQuery({
    queryKey: [
      'marketplace-owner-usage',
      activeChannelID,
      range,
      search,
      rankingSort,
    ],
    queryFn: () =>
      getMyMarketplaceUserUsage({
        channelId: activeChannelID || undefined,
        startTimestamp: range.start
          ? Math.floor(range.start.getTime() / 1000)
          : undefined,
        endTimestamp: range.end
          ? Math.floor(range.end.getTime() / 1000)
          : undefined,
        search: search || undefined,
        sort: rankingSort,
        direction: 'desc',
        page: 1,
        pageSize: 200,
      }),
  })
  const requests = useQuery({
    queryKey: ['marketplace-owner-bargains'],
    queryFn: () => getMyMarketplaceBargainRequests('pending'),
  })
  const multiplierUsers = useQuery({
    queryKey: ['marketplace-owner-multipliers'],
    queryFn: getOwnerMultipliers,
  })
  const [multiplierSelection, setMultiplierSelection] = useState<Set<string>>(
    new Set()
  )
  const [batchMultiplier, setBatchMultiplier] = useState('')
  const batchMultiplierMutation = useMutation({
    mutationFn: batchSetMarketplaceUserMultipliers,
    onError: (error) =>
      toast.error(
        error instanceof Error ? error.message : t('专属倍率更新失败')
      ),
    onSuccess: () => {
      void multiplierUsers.refetch()
      setMultiplierSelection(new Set())
      setBatchMultiplier('')
      toast.success(t('专属倍率批量更新成功，用户已收到通知'))
    },
  })
  const rankedUsers = useMemo(
    () => rankOwnerUsers(usage.data?.items ?? [], activeChannelID, rankingSort),
    [activeChannelID, usage.data?.items, rankingSort]
  )
  const [selectedIDs, setSelectedIDs] = useState<Set<string>>(new Set())
  const [rankStart, setRankStart] = useState('1')
  const [rankEnd, setRankEnd] = useState('')
  const [welfare, setWelfare] = useState<{
    type: WelfareType
    amount: string
  } | null>(null)
  const [result, setResult] = useState<MarketplaceBatchWelfareResult | null>(
    null
  )
  const welfareMutation = useMutation({
    mutationFn: sendMarketplaceBatchWelfare,
    onSuccess: (data) => {
      setResult(data)
      setWelfare(null)
      setSelectedIDs(new Set())
      toast.success(t('批量福利已完成'))
    },
  })
  const selectedUsers = rankedUsers.filter((item) =>
    selectedIDs.has(item.user_id)
  )
  const refresh = () => {
    void usage.refetch()
    void requests.refetch()
    void multiplierUsers.refetch()
  }
  const selectRankRange = () => {
    const start = Math.max(1, Number(rankStart) || 1)
    const end = Math.min(
      rankedUsers.length,
      Number(rankEnd) || rankedUsers.length
    )
    setSelectedIDs(
      new Set(
        rankedUsers
          .slice(start - 1, Math.max(start - 1, end))
          .map((item) => item.user_id)
      )
    )
  }
  return (
    <section className='border-border bg-card mt-3 overflow-hidden rounded-lg border'>
      <header className='flex flex-wrap items-center justify-between gap-3 border-b px-4 py-4'>
        <div>
          <h3 className='font-semibold'>{t('渠道运营')}</h3>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('按渠道用户使用排名发放福利、处理倍率议价并设置分时策略。')}
          </p>
        </div>
        <Button
          variant='outline'
          size='icon'
          title={t('刷新')}
          aria-label={t('刷新渠道运营')}
          onClick={refresh}
        >
          <RefreshCw />
        </Button>
      </header>
      <div className='border-b p-4'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div>
            <h4 className='font-medium'>{t('已调整专属倍率的用户')}</h4>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('包含砍价成功和渠道主手动调整的用户，可批量设置或清除。')}
            </p>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <Input
              className='w-28'
              type='number'
              min='0.001'
              step='0.001'
              placeholder={t('新倍率')}
              aria-label={t('新倍率')}
              value={batchMultiplier}
              onChange={(e) => setBatchMultiplier(e.target.value)}
            />
            <Button
              size='sm'
              disabled={
                !multiplierSelection.size ||
                !Number.isFinite(Number(batchMultiplier)) ||
                Number(batchMultiplier) <= 0 ||
                batchMultiplierMutation.isPending
              }
              onClick={() =>
                batchMultiplierMutation.mutate({
                  targets: [...multiplierSelection].map((key) => {
                    const [channel_id, user] = key.split(':')
                    return { channel_id, user_id: Number(user) }
                  }),
                  multiplier: Number(batchMultiplier),
                })
              }
            >
              {t('批量设置')}
            </Button>
            <Button
              variant='outline'
              size='sm'
              disabled={
                !multiplierSelection.size || batchMultiplierMutation.isPending
              }
              onClick={() =>
                batchMultiplierMutation.mutate({
                  targets: [...multiplierSelection].map((key) => {
                    const [channel_id, user] = key.split(':')
                    return { channel_id, user_id: Number(user) }
                  }),
                  multiplier: null,
                })
              }
            >
              {t('批量清除')}
            </Button>
          </div>
        </div>
        <div className='mt-3 grid gap-2 md:grid-cols-2'>
          {(multiplierUsers.data?.items ?? []).map((item) => {
            const key = `${item.channel_id}:${item.user_id}`
            const checked = multiplierSelection.has(key)
            return (
              <label
                key={key}
                className='border-border flex cursor-pointer items-center gap-2 rounded border px-3 py-2 text-xs'
              >
                <input
                  type='checkbox'
                  checked={checked}
                  onChange={() =>
                    setMultiplierSelection((current) => {
                      const next = new Set(current)
                      if (checked) next.delete(key)
                      else next.add(key)
                      return next
                    })
                  }
                />
                <span className='min-w-0 flex-1'>
                  <b>{item.external_user_id || item.user_id}</b> ·{' '}
                  {item.channel_name}
                  <span className='text-muted-foreground ml-2'>
                    {item.multiplier}×
                  </span>
                </span>
              </label>
            )
          })}
        </div>
        {multiplierUsers.isError && (
          <p role='alert' className='text-destructive mt-3 text-xs'>
            {t('专属倍率用户加载失败，请刷新重试。')}
          </p>
        )}
        {!multiplierUsers.isLoading &&
          !multiplierUsers.isError &&
          !multiplierUsers.data?.items.length && (
            <p className='text-muted-foreground mt-3 text-xs'>
              {t('暂无专属倍率用户')}
            </p>
          )}
      </div>
      <div className='grid divide-y 2xl:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] 2xl:divide-x 2xl:divide-y-0'>
        <BargainRequests query={requests} client={client} />
        <UserWelfarePanel
          channels={manageableChannels}
          activeChannelID={activeChannelID}
          onChannelChange={(value) => {
            setChannelID(value)
            setSelectedIDs(new Set())
          }}
          range={range}
          onRangeChange={(value) => {
            setRange(value)
            setSelectedIDs(new Set())
          }}
          search={searchDraft}
          onSearchChange={setSearchDraft}
          onSearch={() => {
            setSearch(searchDraft.trim())
            setSelectedIDs(new Set())
          }}
          onClearSearch={() => {
            setSearchDraft('')
            setSearch('')
            setSelectedIDs(new Set())
          }}
          summary={usage.data?.summary}
          users={rankedUsers}
          rankingSort={rankingSort}
          onRankingSortChange={(value) => {
            setRankingSort(value)
            setSelectedIDs(new Set())
          }}
          selectedIDs={selectedIDs}
          onSelectionChange={setSelectedIDs}
          rankStart={rankStart}
          rankEnd={rankEnd}
          onRankStartChange={setRankStart}
          onRankEndChange={setRankEnd}
          onSelectRankRange={selectRankRange}
          onWelfare={setWelfare}
          loading={usage.isLoading}
          error={usage.isError}
        />
      </div>
      <TimeMultiplierPanel channelID={activeChannelID} />
      <ConfirmDialog
        open={welfare != null}
        onOpenChange={(open) => !open && setWelfare(null)}
        title={
          welfare?.type === 'transfer'
            ? t('确认批量转账')
            : t('确认批量发放盲盒')
        }
        desc={<WelfareConfirmSummary users={selectedUsers} welfare={welfare} />}
        handleConfirm={() => {
          if (!welfare) return
          void welfareMutation.mutateAsync({
            channelId: activeChannelID,
            userIds: selectedUsers.map((item) =>
              welfare.type === 'blind_box'
                ? item.external_user_id
                : item.user_id
            ),
            type: welfare.type,
            amount: welfare.type === 'transfer' ? Number(welfare.amount) : 1,
          })
        }}
        confirmText={t('确认发放')}
        isLoading={welfareMutation.isPending}
      />
      <WelfareResultDialog result={result} onClose={() => setResult(null)} />
    </section>
  )
}

function BargainRequests(props: {
  query: UseQueryResult<MarketplaceBargainRequestList, Error>
  client: QueryClient
}) {
  const { t } = useTranslation()
  const [pending, setPending] = useState<{
    id: string
    action: 'approve' | 'reject'
  } | null>(null)
  const resolve = useMutation({
    mutationFn: resolveMarketplaceBargainRequest,
    onSuccess: async () => {
      await props.client.invalidateQueries({
        queryKey: ['marketplace-owner-bargains'],
      })
      toast.success(t('议价申请已处理'))
      setPending(null)
    },
  })
  const data = props.query.data
  return (
    <div className='p-4'>
      <h4 className='text-sm font-medium'>{t('待处理倍率议价')}</h4>
      {props.query.isLoading ? (
        <p className='text-muted-foreground mt-3 text-sm'>{t('Loading...')}</p>
      ) : props.query.isError ? (
        <p className='text-destructive mt-3 text-sm'>
          {t('加载议价申请失败，请刷新后重试。')}
        </p>
      ) : data?.items.length ? (
        <div className='mt-3 space-y-2'>
          {data.items.map((item) => (
            <div
              key={item.id}
              className='border-border rounded-md border p-3 text-sm'
            >
              <div className='flex justify-between gap-2'>
                <span>
                  {item.user_external_id || item.user_id} · {item.group_name}
                </span>
                <span>
                  {item.current_multiplier}x → {item.proposed_multiplier}x
                </span>
              </div>
              <p className='text-muted-foreground mt-1 text-xs'>
                {item.reason || t('No reason provided')}
              </p>
              <div className='mt-2 flex gap-2'>
                <Button
                  size='sm'
                  onClick={() => setPending({ id: item.id, action: 'approve' })}
                >
                  <Check />
                  {t('批准')}
                </Button>
                <Button
                  size='sm'
                  variant='outline'
                  onClick={() => setPending({ id: item.id, action: 'reject' })}
                >
                  <X />
                  {t('拒绝')}
                </Button>
              </div>
            </div>
          ))}
        </div>
      ) : (
        <p className='text-muted-foreground mt-3 text-sm'>
          {t('暂无待处理申请')}
        </p>
      )}
      <ConfirmDialog
        open={pending != null}
        onOpenChange={(open) => !open && setPending(null)}
        title={
          pending?.action === 'approve' ? t('批准倍率议价') : t('拒绝倍率议价')
        }
        desc={t('确认处理该申请。')}
        handleConfirm={() =>
          pending &&
          void resolve.mutateAsync({ ...pending, resolutionNote: '' })
        }
        confirmText={pending?.action === 'approve' ? t('批准') : t('拒绝')}
        destructive={pending?.action === 'reject'}
        isLoading={resolve.isPending}
      />
    </div>
  )
}

function UserWelfarePanel(props: {
  channels: Array<{ id: string; system_display_name: string }>
  activeChannelID: string
  onChannelChange: (value: string) => void
  range: DateRange
  onRangeChange: (range: DateRange) => void
  search: string
  onSearchChange: (value: string) => void
  onSearch: () => void
  onClearSearch: () => void
  summary?: {
    total_users: number
    total_requests: number
    consumer_amount: number
    owner_income: number
  }
  users: MarketplaceOwnerUsageItem[]
  rankingSort: OwnerUsageSort
  onRankingSortChange: (value: OwnerUsageSort) => void
  selectedIDs: Set<string>
  onSelectionChange: (ids: Set<string>) => void
  rankStart: string
  rankEnd: string
  onRankStartChange: (value: string) => void
  onRankEndChange: (value: string) => void
  onSelectRankRange: () => void
  onWelfare: (value: { type: WelfareType; amount: string }) => void
  loading: boolean
  error: boolean
}) {
  const { t } = useTranslation()
  const [amount, setAmount] = useState('')
  const [type, setType] = useState<WelfareType>('transfer')
  const toggle = (id: string) => {
    const next = new Set(props.selectedIDs)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    props.onSelectionChange(next)
  }
  const toggleAll = () =>
    props.onSelectionChange(
      props.selectedIDs.size === props.users.length
        ? new Set()
        : new Set(props.users.map((item) => item.user_id))
    )
  const valid =
    !props.loading &&
    !props.error &&
    props.selectedIDs.size > 0 &&
    (type === 'blind_box' ||
      (Number.isFinite(Number(amount)) && Number(amount) > 0))
  return (
    <div className='min-w-0 p-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div>
          <h4 className='text-sm font-medium'>{t('渠道用户福利')}</h4>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('按已结算请求次数或消耗额度排名，选择用户后统一发放。')}
          </p>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('消耗额度按请求结算时的余额等值计算，统一余额与套餐口径。')}
          </p>
        </div>
        <Badge variant='outline'>
          {t('已选 {{count}} 人', { count: props.selectedIDs.size })}
        </Badge>
      </div>
      <div className='mt-4 grid gap-3 sm:grid-cols-2'>
        <Select
          value={props.activeChannelID}
          onValueChange={(value) => {
            if (value) props.onChannelChange(value)
          }}
        >
          <SelectTrigger className='w-full min-w-0' aria-label={t('选择渠道')}>
            <SelectValue placeholder={t('选择渠道')}>
              {props.channels.find(
                (channel) => channel.id === props.activeChannelID
              )?.system_display_name || t('选择渠道')}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {props.channels.map((channel) => (
              <SelectItem key={channel.id} value={channel.id}>
                {channel.system_display_name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select
          value={props.rankingSort}
          onValueChange={(value) => {
            if (value === 'requests' || value === 'amount')
              props.onRankingSortChange(value)
          }}
        >
          <SelectTrigger className='w-full' aria-label={t('排名依据')}>
            <SelectValue>
              {props.rankingSort === 'requests'
                ? t('按请求次数排名')
                : t('按消耗额度排名')}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='requests'>{t('按请求次数排名')}</SelectItem>
            <SelectItem value='amount'>{t('按消耗额度排名')}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className='mt-3 flex flex-col gap-2 lg:flex-row'>
        <form
          className='flex min-w-0 flex-1'
          onSubmit={(event) => {
            event.preventDefault()
            props.onSearch()
          }}
        >
          <Input
            value={props.search}
            onChange={(event) => props.onSearchChange(event.target.value)}
            placeholder={t('搜索用户外部 ID、用户 ID 或渠道')}
            aria-label={t('搜索用户')}
            className='rounded-r-none'
          />
          <Button
            type='submit'
            variant='outline'
            size='icon'
            className='rounded-l-none border-l-0'
            aria-label={t('搜索')}
          >
            <Search />
          </Button>
        </form>
        <CompactDateTimeRangePicker
          start={props.range.start}
          end={props.range.end}
          onChange={props.onRangeChange}
          className='w-full lg:w-[18.5rem]'
        />
        {(props.range.start || props.range.end || props.search) && (
          <Button
            variant='outline'
            size='icon'
            onClick={() => {
              props.onRangeChange({})
              props.onClearSearch()
            }}
            title={t('清除筛选')}
            aria-label={t('清除筛选')}
          >
            <RotateCcw />
          </Button>
        )}
      </div>
      <div className='bg-border mt-3 grid grid-cols-2 gap-px overflow-hidden rounded-md border sm:grid-cols-4'>
        {[
          [t('用户数'), props.summary?.total_users ?? 0],
          [t('请求次数'), props.summary?.total_requests ?? 0],
          [t('用户扣费'), formatQuota(props.summary?.consumer_amount ?? 0)],
          [t('渠道收入'), formatQuota(props.summary?.owner_income ?? 0)],
        ].map(([label, value]) => (
          <div key={String(label)} className='bg-card px-3 py-2'>
            <div className='text-muted-foreground text-xs'>{label}</div>
            <div className='mt-1 font-semibold tabular-nums'>
              {typeof value === 'number' ? value.toLocaleString() : value}
            </div>
          </div>
        ))}
      </div>
      <div className='mt-3 grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-end gap-2'>
        <label className='min-w-0 space-y-1 text-xs'>
          <span className='text-muted-foreground'>{t('起始排名')}</span>
          <Input
            type='number'
            min={1}
            value={props.rankStart}
            onChange={(event) => props.onRankStartChange(event.target.value)}
            aria-label={t('起始排名')}
          />
        </label>
        <label className='min-w-0 space-y-1 text-xs'>
          <span className='text-muted-foreground'>{t('结束排名')}</span>
          <Input
            type='number'
            min={1}
            value={props.rankEnd}
            placeholder={t('末位')}
            onChange={(event) => props.onRankEndChange(event.target.value)}
            aria-label={t('结束排名')}
          />
        </label>
        <Button
          variant='outline'
          disabled={props.loading || props.error || !props.users.length}
          onClick={props.onSelectRankRange}
        >
          {t('选择排名')}
        </Button>
      </div>
      {props.error && (
        <p role='alert' className='text-destructive py-6 text-sm'>
          {t('用户使用记录加载失败，请刷新重试。')}
        </p>
      )}
      {props.loading && (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {t('Loading...')}
        </p>
      )}
      {!props.loading && !props.error && props.users.length === 0 && (
        <p className='text-muted-foreground py-8 text-center text-sm'>
          {t('该渠道还没有可用于福利发放的用户使用记录。')}
        </p>
      )}
      {!props.loading && !props.error && props.users.length > 0 && (
        <div className='mt-3 max-h-80 overflow-auto border-y'>
          <div className='bg-card sticky top-0 grid grid-cols-[1rem_2rem_minmax(5rem,1fr)_4rem_5rem_6rem_6rem] items-center gap-2 border-b px-2 py-2 text-xs font-medium'>
            <input
              type='checkbox'
              checked={props.selectedIDs.size === props.users.length}
              onChange={toggleAll}
              aria-label={t('全选用户')}
            />
            <span>#</span>
            <span>{t('用户')}</span>
            <span className='text-right'>{t('请求次数')}</span>
            <span className='text-right'>{t('成功率')}</span>
            <span className='text-right'>{t('消耗额度')}</span>
            <span className='text-right'>{t('渠道收入')}</span>
          </div>
          {props.users.map((item, index) => (
            <label
              key={item.user_id}
              className='hover:bg-muted/50 grid cursor-pointer grid-cols-[1rem_2rem_minmax(5rem,1fr)_4rem_5rem_6rem_6rem] items-center gap-2 border-b px-2 py-3 text-sm last:border-b-0'
            >
              <input
                type='checkbox'
                checked={props.selectedIDs.has(item.user_id)}
                onChange={() => toggle(item.user_id)}
              />
              <span className='text-muted-foreground tabular-nums'>
                {index + 1}
              </span>
              <span className='truncate'>
                {item.external_user_id || item.user_id}
              </span>
              <span className='text-right tabular-nums'>
                {item.request_count.toLocaleString()}
              </span>
              <span className='text-right tabular-nums'>
                {(item.success_rate * 100).toFixed(1)}%
              </span>
              <span className='text-right tabular-nums'>
                {formatQuota(item.total_settlement_gross_amount)}
              </span>
              <span className='text-right tabular-nums'>
                {formatQuota(item.total_owner_income)}
              </span>
            </label>
          ))}
        </div>
      )}
      <div className='mt-3 grid gap-2 sm:grid-cols-[minmax(0,1fr)_9rem_auto]'>
        <Select
          value={type}
          onValueChange={(value) => {
            if (value) setType(value as WelfareType)
          }}
        >
          <SelectTrigger className='w-full' aria-label={t('福利类型')}>
            <SelectValue>
              {type === 'transfer' ? t('批量转账') : t('批量发盲盒')}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='transfer'>{t('批量转账')}</SelectItem>
            <SelectItem value='blind_box'>{t('批量发盲盒')}</SelectItem>
          </SelectContent>
        </Select>
        {type === 'transfer' ? (
          <Input
            type='number'
            min={1}
            value={amount}
            onChange={(event) => setAmount(event.target.value)}
            placeholder={t('每人额度')}
          />
        ) : (
          <div className='text-muted-foreground flex items-center text-xs'>
            {t('每人发放 1 个盲盒')}
          </div>
        )}
        <Button
          disabled={!valid}
          onClick={() => props.onWelfare({ type, amount })}
        >
          <Send />
          {t('发放福利')}
        </Button>
      </div>
    </div>
  )
}

function WelfareConfirmSummary(props: {
  users: MarketplaceOwnerUsageItem[]
  welfare: { type: WelfareType; amount: string } | null
}) {
  const { t } = useTranslation()
  if (!props.welfare) return null
  const isTransfer = props.welfare.type === 'transfer'
  return (
    <div className='space-y-2 text-sm'>
      <p>
        {isTransfer
          ? t('将向 {{count}} 位已选用户分别转入 {{amount}} 额度。', {
              count: props.users.length,
              amount: props.welfare.amount,
            })
          : t('将向 {{count}} 位已选用户各发放 1 个盲盒。', {
              count: props.users.length,
            })}
      </p>
      <p className='text-muted-foreground'>
        {t('发放后将展示每位用户的处理结果。')}
      </p>
    </div>
  )
}

function WelfareResultDialog(props: {
  result: MarketplaceBatchWelfareResult | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  return (
    <ConfirmDialog
      open={props.result != null}
      onOpenChange={(open) => !open && props.onClose()}
      title={t('批量福利结果')}
      desc={
        <div className='space-y-3'>
          <div className='flex gap-2'>
            <Badge>
              {t('成功 {{count}}', { count: props.result?.success_count ?? 0 })}
            </Badge>
            <Badge variant='destructive'>
              {t('失败 {{count}}', { count: props.result?.failed_count ?? 0 })}
            </Badge>
          </div>
          <div className='max-h-48 space-y-1 overflow-y-auto text-xs'>
            {props.result?.details.map((item) => (
              <p
                key={`${item.user_id}-${item.status}`}
                className={
                  item.status === 'success'
                    ? 'text-emerald-700 dark:text-emerald-400'
                    : 'text-destructive'
                }
              >
                {item.user_id}:{' '}
                {item.status === 'success'
                  ? t('成功')
                  : item.error || t('失败')}
              </p>
            ))}
          </div>
        </div>
      }
      handleConfirm={props.onClose}
      confirmText={t('完成')}
      cancelBtnText={t('关闭')}
    />
  )
}

function TimeMultiplierPanel(props: { channelID: string }) {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['marketplace-time-multipliers', props.channelID],
    queryFn: () => getMarketplaceTimeRangeMultipliers(props.channelID),
    enabled: Boolean(props.channelID),
  })
  const client = useQueryClient()
  const [rule, setRule] = useState({
    start: '',
    end: '',
    multiplier: '1',
    label: '',
  })
  const add = useMutation({
    mutationFn: createMarketplaceTimeRangeMultiplier,
    onSuccess: () => {
      void client.invalidateQueries({
        queryKey: ['marketplace-time-multipliers', props.channelID],
      })
      setRule({ start: '', end: '', multiplier: '1', label: '' })
    },
  })
  const remove = useMutation({
    mutationFn: deleteMarketplaceTimeRangeMultiplier,
    onSuccess: () =>
      void client.invalidateQueries({
        queryKey: ['marketplace-time-multipliers', props.channelID],
      }),
  })
  if (!props.channelID) return null
  return (
    <div className='border-t p-4'>
      <h4 className='text-sm font-medium'>{t('分时倍率')}</h4>
      <div className='mt-3 grid gap-2 sm:grid-cols-4'>
        <Input
          type='datetime-local'
          value={rule.start}
          onChange={(event) => setRule({ ...rule, start: event.target.value })}
        />
        <Input
          type='datetime-local'
          value={rule.end}
          onChange={(event) => setRule({ ...rule, end: event.target.value })}
        />
        <Input
          type='number'
          min={0.01}
          step='0.01'
          value={rule.multiplier}
          onChange={(event) =>
            setRule({ ...rule, multiplier: event.target.value })
          }
        />
        <Input
          placeholder={t('说明')}
          value={rule.label}
          onChange={(event) => setRule({ ...rule, label: event.target.value })}
        />
      </div>
      <Button
        className='mt-2'
        size='sm'
        disabled={
          !rule.start ||
          !rule.end ||
          Number(rule.multiplier) <= 0 ||
          add.isPending
        }
        onClick={() =>
          void add.mutateAsync({
            channelId: props.channelID,
            startTimestamp: Math.floor(new Date(rule.start).getTime() / 1000),
            endTimestamp: Math.floor(new Date(rule.end).getTime() / 1000),
            multiplier: Number(rule.multiplier),
            label: rule.label,
          })
        }
      >
        {t('添加分时倍率')}
      </Button>
      <div className='mt-3 space-y-1'>
        {query.data?.map((item) => (
          <div
            key={item.id}
            className='flex items-center justify-between text-xs'
          >
            <span>
              {new Date(item.start_timestamp * 1000).toLocaleString()} -{' '}
              {new Date(item.end_timestamp * 1000).toLocaleString()} ·{' '}
              {item.multiplier}x {item.label}
            </span>
            <Button
              variant='ghost'
              size='icon'
              title={t('删除')}
              onClick={() =>
                void remove.mutateAsync({
                  channelId: props.channelID,
                  ruleId: item.id,
                })
              }
            >
              <X />
            </Button>
          </div>
        ))}
      </div>
    </div>
  )
}
