import {
  Activity,
  CirclePause,
  Loader2,
  Pause,
  Pencil,
  Play,
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useMarketplaceMutations } from '../hooks'
import { failedConnectivityModels, hasGPT56Model } from '../lib/verification'
import type { MarketplaceChannel } from '../types'

export function OwnerChannelActions(props: {
  channel: MarketplaceChannel
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const mutations = useMarketplaceMutations()
  const channel = props.channel

  const needsDetection = hasGPT56Model(channel.declared_models)
  const failedConnectivityCount = failedConnectivityModels(
    channel.model_verification_results
  ).length
  const verificationRunning =
    ['queued', 'running'].includes(channel.gpt56_mapping_status) ||
    ['queued', 'running'].includes(channel.connectivity_test_status)
  const act = async (
    action:
      | 'detect'
      | 'test-connectivity'
      | 'retry-connectivity'
      | 'pause-verification'
      | 'pause'
      | 'resume'
  ) => {
    try {
      if (action === 'detect') {
        await mutations.detect.mutateAsync(channel.id)
        toast.info(t('GPT-5.6 检测已开始，页面会自动更新结果'))
        return
      }
      if (action === 'test-connectivity') {
        await mutations.testConnectivity.mutateAsync(channel.id)
        toast.info(t('模型连通性测试已开始，页面会自动更新结果'))
        return
      }
      if (action === 'retry-connectivity') {
        await mutations.retryConnectivity.mutateAsync(channel.id)
        toast.info(
          t('正在重新测试 {{count}} 个失败模型', {
            count: failedConnectivityCount,
          })
        )
        return
      }
      if (action === 'pause-verification') {
        await mutations.pauseVerification.mutateAsync(channel.id)
        toast.success(t('检测已暂停，可以重新开始'))
        return
      }
      await mutations.pause.mutateAsync({
        id: channel.id,
        paused: action === 'pause',
      })
      toast.success(action === 'pause' ? t('渠道已暂停') : t('渠道已恢复'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('操作失败'))
    }
  }

  return (
    <div className='flex shrink-0 flex-wrap items-center gap-2 lg:justify-end'>
      <Button variant='outline' size='sm' onClick={props.onEdit}>
        <Pencil />
        {t('编辑')}
      </Button>
      {needsDetection && (
        <Button
          variant='outline'
          size='sm'
          onClick={() => void act('detect')}
          disabled={
            mutations.detect.isPending ||
            ['queued', 'running'].includes(channel.gpt56_mapping_status)
          }
        >
          <ShieldCheck
            className={cn(
              ['queued', 'running'].includes(channel.gpt56_mapping_status) &&
                'animate-pulse'
            )}
          />
          {['queued', 'running'].includes(channel.gpt56_mapping_status)
            ? t('检测中')
            : t('检测 GPT-5.6')}
        </Button>
      )}
      <Button
        variant='outline'
        size='sm'
        onClick={() =>
          void act(
            failedConnectivityCount > 0
              ? 'retry-connectivity'
              : 'test-connectivity'
          )
        }
        disabled={
          mutations.testConnectivity.isPending ||
          mutations.retryConnectivity.isPending ||
          ['queued', 'running'].includes(channel.connectivity_test_status)
        }
      >
        <Activity
          className={cn(
            ['queued', 'running'].includes(channel.connectivity_test_status) &&
              'animate-pulse'
          )}
        />
        {['queued', 'running'].includes(channel.connectivity_test_status)
          ? t('测试中')
          : failedConnectivityCount > 0
            ? t('重试失败模型（{{count}}）', {
                count: failedConnectivityCount,
              })
            : t('测试连通性')}
      </Button>
      {verificationRunning && (
        <Button
          variant='outline'
          size='sm'
          onClick={() => void act('pause-verification')}
          disabled={mutations.pauseVerification.isPending}
        >
          <CirclePause />
          {mutations.pauseVerification.isPending
            ? t('正在暂停')
            : t('暂停检测')}
        </Button>
      )}
      {channel.lifecycle_status === 'suspended' ? (
        <Button variant='outline' size='sm' onClick={() => void act('resume')}>
          <Play />
          {t('恢复')}
        </Button>
      ) : (
        <Button
          variant='outline'
          size='sm'
          onClick={() => void act('pause')}
          disabled={
            channel.lifecycle_status !== 'active' &&
            channel.lifecycle_status !== 'degraded'
          }
        >
          <Pause />
          {t('暂停')}
        </Button>
      )}
      <Button
        variant='ghost'
        size='sm'
        className='text-destructive hover:text-destructive'
        onClick={props.onDelete}
      >
        <Trash2 />
        {t('删除')}
      </Button>
      {(mutations.pause.isPending ||
        mutations.detect.isPending ||
        mutations.testConnectivity.isPending ||
        mutations.retryConnectivity.isPending ||
        mutations.pauseVerification.isPending) && (
        <Loader2 className='text-muted-foreground size-4 animate-spin' />
      )}
    </div>
  )
}
