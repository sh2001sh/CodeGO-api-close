import { ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Switch } from '@/components/ui/switch'
import { useMarketplaceChannelUpdate } from '../hooks'
import type { MarketplaceChannel } from '../types'

export function SensitiveWordPolicyControl(props: {
  channel: MarketplaceChannel
  admin?: boolean
}) {
  const { t } = useTranslation()
  const update = useMarketplaceChannelUpdate(props.admin === true)
  const pendingValue =
    update.variables?.values.sensitive_word_interception_enabled
  const enabled =
    update.isPending && typeof pendingValue === 'boolean'
      ? pendingValue
      : props.channel.sensitive_word_interception_enabled

  const toggle = async (checked: boolean) => {
    try {
      await update.mutateAsync({
        id: props.channel.id,
        values: { sensitive_word_interception_enabled: checked },
      })
      toast.success(
        checked ? t('该分组已开启敏感词拦截') : t('该分组已关闭敏感词拦截')
      )
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('保存失败'))
    }
  }

  return (
    <div className='border-border/70 mt-3 flex flex-wrap items-center justify-between gap-3 border-t pt-3'>
      <div className='flex min-w-0 items-start gap-2'>
        <ShieldAlert className='text-muted-foreground mt-0.5 size-4 shrink-0' />
        <div>
          <p className='text-xs font-medium'>{t('敏感词拦截')}</p>
          <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
            {enabled
              ? t('该分组请求命中平台敏感词规则时会被拦截。')
              : t('该分组跳过平台敏感词检测，由渠道主承担内容治理责任。')}
          </p>
        </div>
      </div>
      <Switch
        checked={enabled}
        disabled={update.isPending}
        onCheckedChange={(checked) => void toggle(checked)}
        aria-label={t('敏感词拦截')}
      />
    </div>
  )
}
