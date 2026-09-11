import { lazy, Suspense, useState } from 'react'
import {
  ChartNoAxesCombined,
  Users,
  ScrollText,
  ShieldAlert,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
  return (
    <section className='mt-6 space-y-4' aria-label={t('渠道主管理')}>
      <Tabs value={tab} onValueChange={setTab}>
        <TabsList className='dawn-owner-tabs' aria-label={t('渠道主管理导航')}>
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
