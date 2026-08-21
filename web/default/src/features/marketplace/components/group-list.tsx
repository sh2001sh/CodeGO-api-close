import { useMemo, useState } from 'react'
import { ShieldCheck, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  useMarketplaceAutoRoutePool,
  useMarketplaceAutoRoutePoolUpdate,
} from '../hooks'
import {
  appendAutoRoutePoolGroup,
  selectedAutoRoutePoolGroupIDs,
} from '../lib/auto-route-pool'
import type { MarketplaceGroup } from '../types'
import { GroupMarketItem } from './group-rows'

export function MarketplaceGroupList(props: {
  groups: MarketplaceGroup[]
  loading: boolean
  error: boolean
  routePoolEnabled?: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState('')
  const [addingGroupID, setAddingGroupID] = useState('')
  const autoPool = useMarketplaceAutoRoutePool(props.routePoolEnabled)
  const autoPoolUpdate = useMarketplaceAutoRoutePoolUpdate()
  const selectedGroupIDs = useMemo(
    () => selectedAutoRoutePoolGroupIDs(autoPool.data?.items ?? []),
    [autoPool.data?.items]
  )
  const selectedGroups = useMemo(
    () => new Set(selectedGroupIDs),
    [selectedGroupIDs]
  )

  if (props.loading) return <GroupListSkeleton />
  if (props.error) return <GroupListError onRetry={props.onRetry} />
  if (props.groups.length === 0) return <GroupListEmpty />

  const toggle = (groupID: string) =>
    setExpanded((current) => (current === groupID ? '' : groupID))
  const addToRoutePool = async (groupID: string) => {
    if (addingGroupID || selectedGroups.has(groupID)) return
    setAddingGroupID(groupID)
    try {
      const pool = autoPool.data ?? (await autoPool.refetch()).data
      if (!pool) throw new Error(t('无法读取 Auto 路由池'))
      if (
        pool.items.some((item) => item.group_id === groupID && item.selected)
      ) {
        return
      }
      const nextGroupIDs = appendAutoRoutePoolGroup(pool.items, groupID)
      await autoPoolUpdate.mutateAsync(nextGroupIDs)
      toast.success(t('已添加到 Auto 路由池'))
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('添加到路由池失败')
      )
    } finally {
      setAddingGroupID('')
    }
  }

  return (
    <div className='bg-muted/25 space-y-1.5 p-2'>
      {props.groups.map((group) => (
        <GroupMarketItem
          key={group.id}
          group={group}
          open={expanded === group.id}
          onToggle={() => toggle(group.id)}
          routePoolSelected={selectedGroups.has(group.id)}
          routePoolBusy={Boolean(addingGroupID) || autoPool.isLoading}
          routePoolAdding={addingGroupID === group.id}
          onAddToRoutePool={() => void addToRoutePool(group.id)}
          showRoutePoolAction={props.routePoolEnabled !== false}
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
        <Skeleton key={index} className='h-20 w-full rounded-md' />
      ))}
    </div>
  )
}
