import { ShieldCheck, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { MarketplaceGroup } from '../types'
import { GroupBatchTestPanel } from './group-batch-test-panel'
import { GroupMarketItem } from './group-rows'
import { useGroupListController } from './use-group-list-controller'

type MarketplaceGroupListProps = {
  groups: MarketplaceGroup[]
  loading: boolean
  error: boolean
  routePoolEnabled?: boolean
  routePoolID?: string
  onRetry: () => void
}

type GroupListController = ReturnType<typeof useGroupListController>

/** Renders marketplace groups with batch testing and route pool controls. */
export function MarketplaceGroupList(props: MarketplaceGroupListProps) {
  const controller = useGroupListController(
    props.groups,
    props.routePoolEnabled,
    props.routePoolID
  )

  if (props.loading) return <GroupListSkeleton />
  if (props.error) return <GroupListError onRetry={props.onRetry} />
  if (props.groups.length === 0) return <GroupListEmpty />
  return <ReadyGroupList props={props} controller={controller} />
}

function ReadyGroupList(input: {
  props: MarketplaceGroupListProps
  controller: GroupListController
}) {
  const { props, controller } = input
  return (
    <div className='bg-muted/25 space-y-1.5 p-2'>
      <GroupBatchTestPanel
        selectedGroupIDs={controller.selection.selectedGroupIDs}
        availableModels={controller.selection.availableModels}
        selectedModel={controller.selection.selectedModel}
        testState={controller.selection.testState}
        testResults={controller.selection.testResults}
        selectedResultGroupIDs={controller.selection.selectedResultGroupIDs}
        routeAdding={controller.routePool.routeAdding}
        batchModel={controller.selection.batchQuery.data?.model}
        items={controller.selection.items}
        onModelChange={controller.selection.setTestModel}
        onRun={() => void controller.runBatchTest()}
        onTogglePassed={controller.selectionActions.togglePassed}
        onAddPassed={() => void controller.addPassedGroups()}
        onReset={controller.selectionActions.reset}
        onToggleResult={controller.selectionActions.toggleResult}
      />
      <div className='space-y-1.5'>
        {props.groups.map((group) => (
          <GroupMarketItem
            key={group.id}
            group={group}
            open={controller.expanded === group.id}
            onToggle={() => controller.toggleExpanded(group.id)}
            routePoolSelected={controller.routePool.selectedGroups.has(
              group.id
            )}
            routePoolBusy={
              Boolean(controller.routePool.addingGroupID) ||
              controller.routePool.query.isLoading
            }
            routePoolAdding={controller.routePool.addingGroupID === group.id}
            onAddToRoutePool={() => void controller.addSingleGroup(group.id)}
            showRoutePoolAction={props.routePoolEnabled !== false}
            selectable={controller.selection.selectableGroups.some(
              (item) => item.id === group.id
            )}
            selected={controller.selection.selectedGroupIDs.includes(group.id)}
            selectionDisabled={
              controller.selection.selectedGroupIDs.length >= 5 &&
              !controller.selection.selectedGroupIDs.includes(group.id)
            }
            onSelect={() =>
              controller.selectionActions.toggleSelected(group.id)
            }
          />
        ))}
      </div>
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
      <div className='border-primary/30 text-primary bg-primary/[0.04] flex size-12 items-center justify-center rounded-xl border'>
        <Sparkles className='size-5' />
      </div>
      <div className='mt-4 font-medium'>{t('等待首批公开渠道')}</div>
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
