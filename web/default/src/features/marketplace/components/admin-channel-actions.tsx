import {
  Activity,
  CirclePause,
  Pencil,
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { useAdminMarketplaceVerification } from '../hooks'
import { failedConnectivityModels, hasGPT56Model } from '../lib/verification'
import type { MarketplaceChannel } from '../types'

export function AdminChannelActions(props: {
  channel: MarketplaceChannel
  onEdit: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation()
  const detection = useAdminMarketplaceVerification('detect')
  const connectivityTest = useAdminMarketplaceVerification('test')
  const connectivityRetry = useAdminMarketplaceVerification('retry-test')
  const verificationPause = useAdminMarketplaceVerification('pause')
  const channel = props.channel
  const failedCount = failedConnectivityModels(
    channel.model_verification_results
  ).length
  const connectivityRunning = ['queued', 'running'].includes(
    channel.connectivity_test_status
  )

  const runConnectivityTest = () => {
    const mutation = failedCount > 0 ? connectivityRetry : connectivityTest
    mutation.mutate(channel.id, {
      onSuccess: () =>
        toast.info(
          failedCount > 0
            ? t('正在重新测试 {{count}} 个失败模型', {
                count: failedCount,
              })
            : t('模型连通性测试已开始，页面会自动更新结果')
        ),
      onError: (error) =>
        toast.error(error instanceof Error ? error.message : t('测试启动失败')),
    })
  }

  return (
    <div className='flex shrink-0 flex-wrap items-center gap-2'>
      {hasGPT56Model(channel.declared_models) && (
        <Button
          variant='outline'
          size='sm'
          disabled={
            detection.isPending ||
            ['queued', 'running'].includes(channel.gpt56_mapping_status)
          }
          onClick={() => detection.mutate(channel.id)}
        >
          <ShieldCheck />
          {['queued', 'running'].includes(channel.gpt56_mapping_status)
            ? t('检测中')
            : t('检测 GPT-5.6')}
        </Button>
      )}
      <Button
        variant='outline'
        size='sm'
        disabled={
          connectivityTest.isPending ||
          connectivityRetry.isPending ||
          connectivityRunning
        }
        onClick={runConnectivityTest}
      >
        <Activity />
        {connectivityRunning
          ? t('测试中')
          : failedCount > 0
            ? t('重试失败模型（{{count}}）', { count: failedCount })
            : t('测试连通性')}
      </Button>
      {isVerificationRunning(channel) && (
        <Button
          variant='outline'
          size='sm'
          disabled={verificationPause.isPending}
          onClick={() =>
            verificationPause.mutate(channel.id, {
              onSuccess: () => toast.success(t('检测已暂停，可以重新开始')),
              onError: (error) =>
                toast.error(
                  error instanceof Error ? error.message : t('暂停检测失败')
                ),
            })
          }
        >
          <CirclePause />
          {verificationPause.isPending ? t('正在暂停') : t('暂停检测')}
        </Button>
      )}
      <Button variant='outline' size='sm' onClick={props.onEdit}>
        <Pencil />
        {t('编辑渠道')}
      </Button>
      <Button
        variant='ghost'
        size='sm'
        className='text-destructive hover:text-destructive'
        onClick={props.onDelete}
      >
        <Trash2 />
        {t('删除')}
      </Button>
    </div>
  )
}

function isVerificationRunning(channel: MarketplaceChannel) {
  return (
    ['queued', 'running'].includes(channel.gpt56_mapping_status) ||
    ['queued', 'running'].includes(channel.connectivity_test_status)
  )
}
