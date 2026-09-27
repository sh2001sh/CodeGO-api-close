import { lazy, Suspense, useState } from 'react'
import {
  ChartNoAxesCombined,
  Users,
  ScrollText,
  ShieldAlert,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ChannelCreateDialog } from '@/features/marketplace/components/channel-create-dialog'
import { OwnerChannels } from '@/features/marketplace/components/owner-channels'
import { useMyMarketplaceChannels } from '@/features/marketplace/hooks'

const OwnerOperationsPanel = lazy(() =>
  import('@/features/marketplace/components/owner-operations-panel').then(
    (module) => ({ default: module.OwnerOperationsPanel })
  )
)
const OwnerChannelUsageLogs = lazy(() =>
  import('@/features/usage-logs/components/owner-channel-usage-logs').then(
    (module) => ({ default: module.OwnerChannelUsageLogs })
  )
)
const SecurityAuditPanel = lazy(() =>
  import('@/features/security-audit/security-audit-panel').then((module) => ({
    default: module.SecurityAuditPanel,
  }))
)

export function OwnerView() {
  const { t } = useTranslation()
  const [tab, setTab] = useState('channels')
  const [showCreate, setShowCreate] = useState(false)
  const [userFocus, setUserFocus] = useState<{
    channelId: string
    userId: number
    nonce: number
  }>()
  const [logFocus, setLogFocus] = useState<{
    channelId: string
    requestId: string
    userId: number
    nonce: number
  }>()
  const channels = useMyMarketplaceChannels()
  const owned = channels.data ?? []
  const active = owned.filter(
    (item) => !item.deleted_at && item.lifecycle_status === 'active'
  ).length
  const needsAttention = owned.filter(
    (item) =>
      !item.deleted_at &&
      ['degraded', 'suspended', 'pending_review'].includes(
        item.lifecycle_status
      )
  ).length
  const income = owned.reduce((sum, item) => sum + (item.total_income || 0), 0)
  const topChannels = [...owned]
    .sort((a, b) => (b.total_income || 0) - (a.total_income || 0))
    .slice(0, 5)
  const maxIncome = Math.max(
    1,
    ...topChannels.map((item) => item.total_income || 0)
  )
  return (
    <section className='mt-6 space-y-4' aria-label={t('渠道主管理')}>
      <div className='border-border bg-card rounded-lg border p-4 sm:p-5'>
        <div className='flex flex-wrap items-start justify-between gap-3'>
          <div>
            <h2 className='text-lg font-semibold'>{t('渠道主工作台')}</h2>
            <p className='text-muted-foreground mt-1 text-sm'>
              {t('先看渠道状态与收益，再处理用户和调用问题。')}
            </p>
          </div>
          <Button onClick={() => setShowCreate(true)}>{t('添加渠道')}</Button>
        </div>
        <div className='mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4'>
          {[
            [t('我的渠道'), owned.length.toLocaleString()],
            [t('在售渠道'), active.toLocaleString()],
            [t('待处理渠道'), needsAttention.toLocaleString()],
            [t('累计渠道收入'), formatQuota(income)],
          ].map(([label, value]) => (
            <div key={label} className='bg-muted/30 rounded-md px-3 py-3'>
              <div className='text-muted-foreground text-xs'>{label}</div>
              <div className='mt-1 text-xl font-semibold tabular-nums'>
                {channels.isLoading ? '—' : value}
              </div>
            </div>
          ))}
        </div>
        {channels.isError && (
          <p role='alert' className='text-destructive mt-3 text-sm'>
            {t('渠道数据加载失败，请重试。')}
          </p>
        )}
        {topChannels.length > 0 && (
          <div className='mt-5 border-t pt-4'>
            <h3 className='text-sm font-semibold'>{t('各渠道累计收入')}</h3>
            <p className='text-muted-foreground mt-0.5 text-xs'>
              {t('历史累计，最多展示收入最高的 5 个渠道。')}
            </p>
            <div className='mt-3 space-y-2.5'>
              {topChannels.map((item) => (
                <div
                  key={item.id}
                  className='grid grid-cols-[minmax(0,8rem)_minmax(0,1fr)_auto] items-center gap-2 text-xs sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)_auto]'
                >
                  <span className='truncate' title={item.system_display_name}>
                    {item.system_display_name}
                  </span>
                  <div
                    className='bg-muted h-2 rounded-full'
                    role='img'
                    aria-label={`${item.system_display_name}: ${formatQuota(item.total_income || 0)}`}
                  >
                    <div
                      className='bg-primary h-full rounded-full'
                      style={{
                        width: `${Math.max(0, ((item.total_income || 0) / maxIncome) * 100)}%`,
                      }}
                    />
                  </div>
                  <span className='font-medium tabular-nums'>
                    {formatQuota(item.total_income || 0)}
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
        <Tabs value={tab} onValueChange={setTab} className='mt-4 border-t pt-4'>
          <TabsList
            className='dawn-owner-tabs'
            aria-label={t('渠道主管理导航')}
          >
            <TabsTrigger value='channels'>
              <ChartNoAxesCombined aria-hidden='true' />
              {t('渠道与收益')}
            </TabsTrigger>
            <TabsTrigger value='users'>
              <Users aria-hidden='true' />
              {t('用户与倍率')}
            </TabsTrigger>
            <TabsTrigger value='logs'>
              <ScrollText aria-hidden='true' />
              {t('调用日志')}
            </TabsTrigger>
            <TabsTrigger value='security-audit'>
              <ShieldAlert aria-hidden='true' />
              {t('安全审计')}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>
      <Suspense fallback={<Skeleton className='h-64 w-full' />}>
        {tab === 'channels' && (
          <OwnerChannels onAdd={() => setShowCreate(true)} />
        )}
        {tab === 'users' && <OwnerOperationsPanel focus={userFocus} />}
        {tab === 'logs' && (
          <OwnerChannelUsageLogs
            channels={channels.data ?? []}
            focus={logFocus}
          />
        )}
        {tab === 'security-audit' && (
          <SecurityAuditPanel
            channels={channels.data ?? []}
            onInspectUser={(event) => {
              setUserFocus({
                channelId: event.marketplace_channel_id,
                userId: event.user_id,
                nonce: Date.now(),
              })
              setTab('users')
            }}
            onInspectLogs={(event) => {
              setLogFocus({
                channelId: event.marketplace_channel_id,
                requestId: event.request_id,
                userId: event.user_id,
                nonce: Date.now(),
              })
              setTab('logs')
            }}
          />
        )}
      </Suspense>
      <ChannelCreateDialog open={showCreate} onOpenChange={setShowCreate} />
    </section>
  )
}
