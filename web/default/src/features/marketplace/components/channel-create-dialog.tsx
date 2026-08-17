import { Clock3, Percent, ShieldCheck, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ChannelCreateForm } from './channel-create-form'

export function ChannelCreateDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex h-[min(92dvh,900px)] max-h-[calc(100dvh-1rem)] max-w-6xl flex-col gap-0 overflow-hidden p-0 sm:max-w-6xl'>
        <DialogHeader className='border-border shrink-0 border-b px-5 py-4 pr-14 sm:px-6'>
          <DialogTitle>{t('添加新渠道')}</DialogTitle>
          <DialogDescription>
            {t('提交连接信息与服务策略，模型和实际调用检测通过后自动上架。')}
          </DialogDescription>
        </DialogHeader>
        <div className='min-h-0 flex-1 overflow-y-auto'>
          <div className='grid xl:grid-cols-[minmax(0,1fr)_310px]'>
            <ChannelCreateForm onCreated={() => props.onOpenChange(false)} />
            <MarketplaceRules />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function MarketplaceRules() {
  const { t } = useTranslation()
  return (
    <aside className='border-border bg-muted/20 border-t px-5 py-6 xl:border-t-0 xl:border-l xl:px-6'>
      <h3 className='font-semibold'>{t('市场结算规则')}</h3>
      <div className='mt-5 space-y-5'>
        <Rule icon={WalletCards} title={t('支持套餐与通用余额')}>
          {t('第三方分组默认支持套餐和通用余额，并分别按页面所示倍率扣费。')}
        </Rule>
        <Rule icon={Percent} title={t('95% 渠道收入')}>
          {t('平台收取 5% 调用佣金，其余 95% 进入渠道主待结算收入。')}
        </Rule>
        <Rule icon={Clock3} title={t('默认 24 小时释放')}>
          {t('调用完成后进入待结算，默认在 24 小时后释放到账。')}
        </Rule>
        <Rule icon={ShieldCheck} title={t('检测通过自动上架')}>
          {t('固定来源无需人工审核，连接与实际模型调用检测通过后自动上架。')}
        </Rule>
      </div>
    </aside>
  )
}

function Rule(props: {
  icon: typeof ShieldCheck
  title: string
  children: React.ReactNode
}) {
  const Icon = props.icon
  return (
    <div className='flex gap-3'>
      <span className='bg-primary/10 text-primary flex size-8 shrink-0 items-center justify-center rounded-md'>
        <Icon className='size-4' />
      </span>
      <div>
        <div className='text-sm font-medium'>{props.title}</div>
        <p className='text-muted-foreground mt-1 text-xs leading-5'>
          {props.children}
        </p>
      </div>
    </div>
  )
}
