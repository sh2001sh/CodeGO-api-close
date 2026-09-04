import { Link } from '@tanstack/react-router'
import {
  ChevronDown,
  ChevronUp,
  KeyRound,
  ListChecks,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import type { StartStep } from './types'
import type { SetupGuideState } from './use-setup-guide'
import {
  QuickActionItem,
  RequestPreview,
  SetupGuideBackdrop,
  StartStepItem,
} from './setup-guide-parts'

function CompactSetupStrip(props: {
  completedStepCount: number
  totalStepCount: number
  nextStep: StartStep
  onExpand: () => void
}) {
  const Icon = props.nextStep.icon

  return (
    <CardStaggerContainer>
      <CardStaggerItem className='app-page-shell overflow-hidden shadow-none'>
        <div className='flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5'>
          <div className='flex min-w-0 items-center gap-3'>
            <span className='border border-primary/30 text-primary bg-primary/[0.04] flex size-9 shrink-0 items-center justify-center rounded-xl'>
              <ListChecks className='size-4' aria-hidden='true' />
            </span>
            <div className='min-w-0'>
              <div className='flex flex-wrap items-center gap-2'>
                <div className='text-foreground text-sm font-semibold'>
                  快速接入进度
                </div>
                <span className='border-border bg-muted text-muted-foreground rounded-full border px-2 py-0.5 text-xs'>
                  已完成 {props.completedStepCount}/{props.totalStepCount}
                </span>
              </div>
              <div className='text-muted-foreground mt-1 text-sm'>
                下一步：{props.nextStep.title}
              </div>
            </div>
          </div>

          <div className='flex flex-wrap items-center gap-2'>
            <Button size='sm' render={<Link to={props.nextStep.to} />}>
              <Icon data-icon='inline-start' />
              {props.nextStep.title}
            </Button>
            <Button variant='outline' size='sm' onClick={props.onExpand}>
              <ChevronDown data-icon='inline-start' />
              展开接入引导
            </Button>
          </div>
        </div>
      </CardStaggerItem>
    </CardStaggerContainer>
  )
}

function ExpandedSetupGuide(props: { guide: SetupGuideState }) {
  const { guide } = props

  return (
    <CardStaggerContainer className='grid items-stretch gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]'>
      <CardStaggerItem className='app-page-shell h-full overflow-hidden shadow-none'>
        <div className='relative h-full overflow-hidden p-4 sm:p-5'>
          <SetupGuideBackdrop />
          <div className='relative grid gap-5 lg:grid-cols-[minmax(0,1fr)_21rem]'>
            <div className='flex min-w-0 flex-col gap-5'>
              <div className='flex flex-wrap items-start justify-between gap-3'>
                <div className='flex max-w-2xl flex-col gap-1'>
                  <h3 className='text-xl font-semibold tracking-tight sm:text-2xl'>
                    接入流程
                  </h3>
                </div>
                <div className='flex flex-wrap items-center gap-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={guide.onToggle}
                  >
                    <ChevronUp data-icon='inline-start' />
                    收起引导
                  </Button>
                  <Button size='sm' render={<Link to='/keys' />}>
                    <KeyRound data-icon='inline-start' />
                    创建 API Key
                  </Button>
                </div>
              </div>

              <ol className='bg-background/45 rounded-2xl border p-2'>
                {guide.startSteps.map((step, index) => (
                  <StartStepItem
                    key={step.title}
                    step={step}
                    index={index}
                    isLast={index === guide.startSteps.length - 1}
                  />
                ))}
              </ol>
            </div>

            <RequestPreview
              example={guide.requestExample}
              signals={guide.heroSignals}
            />
          </div>
        </div>
      </CardStaggerItem>

      <CardStaggerItem className='app-page-shell h-full p-4 shadow-none sm:p-5'>
        <div className='flex h-full flex-col gap-4'>
          <div className='flex flex-col gap-1'>
            <h3 className='text-lg font-semibold tracking-tight'>
              常用入口
            </h3>
          </div>
          <div className='grid gap-2'>
            {guide.visibleQuickActions.map((action) => (
              <QuickActionItem key={action.title} action={action} />
            ))}
          </div>
        </div>
      </CardStaggerItem>
    </CardStaggerContainer>
  )
}

export function SetupGuideSection(props: { guide: SetupGuideState }) {
  const { guide } = props

  if (guide.setupGuideExpanded) {
    return <ExpandedSetupGuide guide={guide} />
  }

  if (!guide.setupComplete) {
    return (
      <CompactSetupStrip
        completedStepCount={guide.completedStepCount}
        totalStepCount={guide.startSteps.length}
        nextStep={guide.nextSetupStep}
        onExpand={guide.onToggle}
      />
    )
  }

  return null
}
