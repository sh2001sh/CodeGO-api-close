import { useState } from 'react'
import { ShieldCheck, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { MarketplaceGroup } from '../types'
import { GroupMarketItem } from './group-rows'

export function MarketplaceGroupList(props: {
  groups: MarketplaceGroup[]
  loading: boolean
  error: boolean
  onRetry: () => void
}) {
  const [expanded, setExpanded] = useState('')
  if (props.loading) return <GroupListSkeleton />
  if (props.error) return <GroupListError onRetry={props.onRetry} />
  if (props.groups.length === 0) return <GroupListEmpty />

  const toggle = (groupID: string) =>
    setExpanded((current) => (current === groupID ? '' : groupID))

  return (
    <div className='bg-muted/35 space-y-2 p-2'>
      {props.groups.map((group) => (
        <GroupMarketItem
          key={group.id}
          group={group}
          open={expanded === group.id}
          onToggle={() => toggle(group.id)}
        />
      ))}
    </div>
  )
}

function GroupListError(props: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className='flex min-h-64 flex-col items-center justify-center gap-3 px-4 text-center'>
      <div className='bg-destructive/10 text-destructive flex size-11 items-center justify-center rounded-lg'>
        <ShieldCheck className='size-5' />
      </div>
      <div className='font-medium'>{t('分组市场暂时不可用')}</div>
      <p className='text-muted-foreground max-w-md text-sm leading-6'>
        {t('无法加载市场数据，请稍后重试。')}
      </p>
      <Button variant='outline' size='sm' onClick={props.onRetry}>
        {t('重新获取')}
      </Button>
    </div>
  )
}

function GroupListEmpty() {
  const { t } = useTranslation()
  return (
    <div className='flex min-h-64 flex-col items-center justify-center px-4 text-center'>
      <div className='bg-primary/10 text-primary flex size-12 items-center justify-center rounded-xl'>
        <Sparkles className='size-5' />
      </div>
      <div className='mt-4 font-medium'>{t('等待首批公开渠道')}</div>
      <p className='text-muted-foreground mt-1 max-w-lg text-sm leading-6 text-pretty'>
        {t(
          '渠道完成检测与管理员审核后会出现在这里；你也可以在“我的渠道”提交自己的模型通道。'
        )}
      </p>
    </div>
  )
}

function GroupListSkeleton() {
  return (
    <div className='space-y-2 p-4'>
      {Array.from({ length: 6 }).map((_, index) => (
        <Skeleton key={index} className='h-24 w-full rounded-lg' />
      ))}
    </div>
  )
}
