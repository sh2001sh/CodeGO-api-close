import { Building2, Network } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { TabsList, TabsTrigger } from '@/components/ui/tabs'

export type PricingSourceView = 'official' | 'third_party'

export function PricingSourceNavigation(props: {
  officialCount: number
  thirdPartyCount: number
}) {
  const { t } = useTranslation()
  const items = [
    {
      value: 'official',
      icon: Building2,
      label: t('CodeGo 官方'),
      description: t('官方维护的模型与分组'),
      count: props.officialCount,
    },
    {
      value: 'third_party',
      icon: Network,
      label: t('第三方分组'),
      description: t('由渠道主提供，使用通用额度'),
      count: props.thirdPartyCount,
    },
  ] as const

  return (
    <TabsList className='bg-muted/60 grid !h-auto w-full grid-cols-1 gap-1 rounded-lg p-1 sm:grid-cols-2'>
      {items.map((item) => {
        const Icon = item.icon
        return (
          <TabsTrigger
            key={item.value}
            value={item.value}
            className='data-active:bg-background min-h-18 !h-auto justify-start gap-3 px-3 py-3 text-left sm:px-4'
          >
            <span className='bg-background text-muted-foreground data-[active=true]:text-primary flex size-9 shrink-0 items-center justify-center rounded-md border shadow-none'>
              <Icon className='size-4' />
            </span>
            <span className='min-w-0 flex-1'>
              <span className='flex items-center justify-between gap-3'>
                <span className='truncate font-semibold'>{item.label}</span>
                <span className='text-muted-foreground text-xs tabular-nums'>
                  {item.count}
                </span>
              </span>
              <span className='text-muted-foreground mt-0.5 block truncate text-xs font-normal'>
                {item.description}
              </span>
            </span>
          </TabsTrigger>
        )
      })}
    </TabsList>
  )
}
