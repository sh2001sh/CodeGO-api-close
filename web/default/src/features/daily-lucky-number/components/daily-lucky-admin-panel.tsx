import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertCircle, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  backfillAdminDailyLuckyNumbers,
  getAdminDailyLuckyNumberDraws,
  retryAdminDailyLuckyNumberDraw,
  updateAdminDailyLuckyNumberConfig,
} from '../api'
import type {
  DailyLuckyConfig,
  LuckyBackfillResult,
  LuckyDrawAdminPayload,
} from '../types'
import { DailyLuckyAdminConfig } from './daily-lucky-admin-config'
import { DailyLuckyAdminDraws } from './daily-lucky-admin-draws'

const dailyLuckyAdminQueryKey = ['admin', 'daily-lucky-number'] as const

const EMPTY_CONFIG: DailyLuckyConfig = {
  enabled: true,
  timezone: 'Asia/Shanghai',
  draw_hour: 20,
  draw_minute: 0,
  base_reward_1_usd: 1,
  base_reward_2_usd: 10,
  base_reward_3_usd: 50,
  base_reward_4_usd: 100,
  multiplier_lite: 1,
  multiplier_standard: 1.1,
  multiplier_pro: 1.2,
  multiplier_ultra: 1.3,
  jackpot_initial_usd: 100,
  jackpot_increment_usd: 20,
  jackpot_cap_usd: 1000,
  cost_per_usd: 0.1,
  monthly_budget_usd: 0,
}

export function DailyLuckyAdminPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [draftOverride, setDraftOverride] = useState<DailyLuckyConfig | null>(null)
  const [backfillResult, setBackfillResult] = useState<LuckyBackfillResult>()

  const query = useQuery({
    queryKey: [...dailyLuckyAdminQueryKey, page],
    queryFn: async (): Promise<LuckyDrawAdminPayload> => {
      const response = await getAdminDailyLuckyNumberDraws(page)
      if (!response.success || !response.data) {
        throw new Error(response.message || 'Unable to load daily lucky number administration.')
      }
      return response.data
    },
    staleTime: 15 * 1000,
  })

  const draft = draftOverride ?? query.data?.config ?? EMPTY_CONFIG

  const saveMutation = useMutation({
    mutationFn: async (config: DailyLuckyConfig) => {
      const response = await updateAdminDailyLuckyNumberConfig(config)
      if (!response.success || !response.data) {
        throw new Error(response.message || 'Unable to save daily lucky number configuration.')
      }
      return response.data
    },
    onSuccess: async (config) => {
      setDraftOverride(config)
      toast.success(t('Daily lucky number configuration saved.'))
      await queryClient.invalidateQueries({ queryKey: dailyLuckyAdminQueryKey })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t('Unable to save daily lucky number configuration.'))
    },
  })

  const retryMutation = useMutation({
    mutationFn: async (drawId: number) => {
      const response = await retryAdminDailyLuckyNumberDraw(drawId)
      if (!response.success) throw new Error(response.message || t('Unable to retry settlement.'))
    },
    onSuccess: async () => {
      toast.success(t('Settlement retry started.'))
      await queryClient.invalidateQueries({ queryKey: dailyLuckyAdminQueryKey })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t('Unable to retry settlement.'))
    },
  })

  const backfillMutation = useMutation({
    mutationFn: async () => {
      const response = await backfillAdminDailyLuckyNumbers()
      if (!response.success || !response.data) {
        throw new Error(response.message || 'Unable to backfill missing numbers.')
      }
      return response.data
    },
    onSuccess: async (result) => {
      setBackfillResult(result)
      toast.success(t('Missing number backfill completed.'))
      await queryClient.invalidateQueries({ queryKey: dailyLuckyAdminQueryKey })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t('Unable to backfill missing numbers.'))
    },
  })

  if (query.isLoading && !query.data) {
    return (
      <div className='space-y-4'>
        <Skeleton className='h-56 w-full rounded-xl' />
        <Skeleton className='h-52 w-full rounded-xl' />
      </div>
    )
  }

  if (query.isError && !query.data) {
    return (
      <Alert variant='destructive'>
        <AlertCircle aria-hidden='true' />
        <AlertDescription className='flex flex-wrap items-center justify-between gap-3'>
          <span>{t('Unable to load daily lucky number administration.')}</span>
          <Button variant='outline' size='sm' onClick={() => void query.refetch()}>
            {t('Try again')}
          </Button>
        </AlertDescription>
      </Alert>
    )
  }

  const payload = query.data
  if (!payload) return null

  return (
    <div className='space-y-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <div>
          <h2 className='text-foreground text-base font-semibold'>{t('Daily Lucky Number Operations')}</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('Monitor fixed-cost rewards, immutable draw snapshots, and settlement recovery.')}
          </p>
        </div>
        <Button
          variant='outline'
          size='sm'
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
        >
          <RefreshCw
            data-icon='inline-start'
            className={query.isFetching ? 'animate-spin' : undefined}
          />
          {t('Refresh')}
        </Button>
      </div>

      <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
        <AdminMetric label={t('Monthly nominal reward')} value={`$${payload.monthly_nominal_reward_usd.toFixed(2)}`} />
        <AdminMetric label={t('Monthly actual cost')} value={`¥${payload.monthly_actual_cost_cny.toFixed(2)}`} />
        <AdminMetric label={t('Monthly budget')} value={payload.monthly_budget_usd > 0 ? `$${payload.monthly_budget_usd.toFixed(2)}` : t('No budget limit')} />
        <AdminMetric label={t('Budget usage')} value={payload.monthly_budget_usd > 0 ? `${payload.monthly_budget_usage_percent.toFixed(1)}%` : t('Not limited')} />
      </div>

      <DailyLuckyAdminConfig
        value={draft}
        saving={saveMutation.isPending}
        onChange={(patch) =>
          setDraftOverride((current) => ({
            ...(current ?? query.data?.config ?? EMPTY_CONFIG),
            ...patch,
          }))
        }
        onSave={() => saveMutation.mutate(draft)}
      />

      <DailyLuckyAdminDraws
        draws={payload.draws}
        page={payload.page}
        pageSize={payload.page_size}
        total={payload.total}
        loading={query.isFetching}
        retryingId={retryMutation.isPending ? retryMutation.variables : undefined}
        backfillPending={backfillMutation.isPending}
        backfillResult={backfillResult}
        onRetry={(drawId) => retryMutation.mutate(drawId)}
        onBackfill={() => backfillMutation.mutate()}
        onPageChange={setPage}
      />
    </div>
  )
}

function AdminMetric(props: { label: string; value: string }) {
  return (
    <div className='app-page-shell px-4 py-3'>
      <div className='text-muted-foreground text-xs'>{props.label}</div>
      <div className='text-foreground mt-1 font-mono text-lg font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}
