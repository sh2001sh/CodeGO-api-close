import { useTranslation } from 'react-i18next'
import {
  Dialog,
  DialogContent,
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
      <DialogContent
        aria-describedby={undefined}
        className='admin-editor marketplace-channel-editor flex h-[min(92dvh,900px)] max-h-[calc(100dvh-1rem)] max-w-6xl flex-col gap-0 overflow-hidden p-0 sm:max-w-6xl'
      >
        <DialogHeader className='border-border shrink-0 border-b px-5 py-4 pr-14 sm:px-6'>
          <DialogTitle>{t('添加新渠道')}</DialogTitle>
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
  const fields = [
    [t('支付来源'), t('套餐、通用余额')],
    [t('平台佣金'), '5%'],
    [t('基础渠道收入'), '95%'],
    [t('新套餐增额分担'), t('套餐增额率 × 60%')],
    [t('首周邀请返利'), t('合资格实付消费的 3%')],
    [t('冻结期'), t('默认 24 小时')],
    [t('上架条件'), t('连通性及模型调用检测通过')],
    [t('固定来源'), t('自动上架')],
    [t('GPT-5.6 一致性检测'), t('模型声明及映射')],
  ]
  return (
    <aside className='border-border border-t px-5 py-6 xl:border-t-0 xl:border-l xl:px-6'>
      <h3 className='text-xl font-semibold'>{t('市场结算规则')}</h3>
      <dl className='mt-6 divide-y'>
        {fields.map(([label, value]) => (
          <div
            key={label}
            className='flex flex-wrap justify-between gap-3 py-4'
          >
            <dt className='text-muted-foreground'>{label}</dt>
            <dd className='max-w-48 text-right font-medium'>{value}</dd>
          </div>
        ))}
      </dl>
    </aside>
  )
}
