import i18n from '@/i18n/config'
import { ArrowRight } from 'lucide-react'
import { motion, useReducedMotion, type Variants } from 'motion/react'
import { useTranslation, Trans } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import type { BlindBoxProp, BlindBoxRecord, BlindBoxStatistics } from '../types'
import { formatBlindBoxTimestamp } from './blind-box-dialog-data'
import { BlindBoxStatsPanel } from './blind-box-notices'

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

const STACK: Variants = {
  initial: {},
  animate: { transition: { staggerChildren: 0.08, delayChildren: 0.04 } },
}

const STACK_ITEM: Variants = {
  initial: { opacity: 0, y: 12 },
  animate: {
    opacity: 1,
    y: 0,
    transition: { duration: 0.4, ease: EASE_OUT_QUINT },
  },
}

const REDUCED_STACK: Variants = { initial: {}, animate: {} }
const REDUCED_ITEM: Variants = {
  initial: { opacity: 0 },
  animate: { opacity: 1, transition: { duration: 0.18 } },
}

export function BlindBoxSidebar(props: {
  quota: number
  availableBoxes: number
  pendingBoxes: number
  records: BlindBoxRecord[]
  props: BlindBoxProp[]
  statistics?: BlindBoxStatistics
  onOpenHistory: () => void
  onOpenProps: () => void
}) {
  const reduced = useReducedMotion()

  return (
    <motion.aside
      className='space-y-4'
      variants={reduced ? REDUCED_STACK : STACK}
      initial='initial'
      animate='animate'
    >
      <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
        <AssetBoard
          quota={props.quota}
          availableBoxes={props.availableBoxes}
          pendingBoxes={props.pendingBoxes}
        />
      </motion.div>

      <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
        <PropsPreview props={props.props} onOpenProps={props.onOpenProps} />
      </motion.div>

      <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
        <BlindBoxStatsPanel statistics={props.statistics} />
      </motion.div>

      <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
        <SettlementCard />
      </motion.div>

      <motion.div variants={reduced ? REDUCED_ITEM : STACK_ITEM}>
        <div className='codego-panel p-4'>
          <div className='flex items-center justify-between gap-2'>
            <div className='flex items-center gap-2.5'>
              <span aria-hidden className='bg-primary block h-3 w-[3px]' />
              <div className='text-foreground text-[13px] font-semibold'>
                <Trans i18nKey={'开奖历史'} />
              </div>
            </div>
            <span className='codego-stat-label'>30D</span>
          </div>
          {props.records[0] ? (
            <div className='border-border/60 mt-3 border-t pt-3'>
              <div className='codego-stat-label'>
                <Trans i18nKey={'最近获得'} />
              </div>
              <div className='text-foreground mt-1.5 truncate text-sm font-medium'>
                {props.records[0].reward_title}
              </div>
              <div className='text-muted-foreground mt-0.5 font-mono text-[10px] tabular-nums'>
                {formatBlindBoxTimestamp(props.records[0].create_time)}
              </div>
            </div>
          ) : (
            <div className='codego-empty mt-3 justify-start py-4 text-left'>
              <span aria-hidden className='bg-border block h-5 w-px' />
              <Trans i18nKey={'暂无记录'} />
            </div>
          )}
          <Button
            type='button'
            variant='outline'
            className='mt-4 w-full justify-between'
            onClick={props.onOpenHistory}
          >
            <Trans i18nKey={'查看最近 30 天记录'} />
            <ArrowRight className='size-4' />
          </Button>
        </div>
      </motion.div>
    </motion.aside>
  )
}

const PROP_STATUS_LABEL: Record<BlindBoxProp['status'], string> = {
  available: '可使用',
  active: '生效中',
  reserved: '已锁定',
  used: '已使用',
  expired: '已过期',
}

function PropsPreview(props: {
  props: BlindBoxProp[]
  onOpenProps: () => void
}) {
  const usable = props.props.filter(
    (prop) => prop.status === 'available' || prop.status === 'active'
  )
  const preview = (usable.length > 0 ? usable : props.props).slice(0, 3)

  return (
    <div className='codego-panel p-4'>
      <div className='flex items-center justify-between gap-2'>
        <div className='flex items-center gap-2.5'>
          <span aria-hidden className='bg-primary block h-3 w-[3px]' />
          <div className='text-foreground text-[13px] font-semibold'>
            <Trans i18nKey={'我的道具'} />
          </div>
        </div>
        {usable.length > 0 ? (
          <span className='codego-stat-label border-primary/30 text-primary border px-1.5 py-0.5'>
            <Trans i18nKey={'可用'} /> {usable.length}
          </span>
        ) : null}
      </div>

      {preview.length > 0 ? (
        <ul className='mt-3 space-y-1.5'>
          {preview.map((prop) => (
            <li
              key={prop.id}
              className='border-border/60 flex items-center justify-between gap-2 border-b py-2 last:border-b-0'
            >
              <span className='text-foreground min-w-0 truncate text-xs font-medium'>
                {prop.title}
              </span>
              <span
                className={cn(
                  'shrink-0 text-[10px] font-semibold',
                  prop.status === 'active'
                    ? 'text-success'
                    : prop.status === 'available'
                      ? 'text-primary'
                      : 'text-muted-foreground'
                )}
              >
                {PROP_STATUS_LABEL[prop.status] || prop.status}
              </span>
            </li>
          ))}
        </ul>
      ) : (
        <div className='codego-empty mt-2 justify-center py-6'>
          <span aria-hidden className='bg-border block h-6 w-px' />
          <Trans i18nKey={'暂无道具'} />
        </div>
      )}

      <Button
        type='button'
        variant='outline'
        className='mt-3 w-full justify-between'
        onClick={props.onOpenProps}
      >
        <Trans i18nKey={'管理全部道具'} />
        <ArrowRight className='size-4' />
      </Button>
    </div>
  )
}

function AssetBoard(props: {
  quota: number
  availableBoxes: number
  pendingBoxes: number
}) {
  useTranslation()
  return (
    <div className='codego-panel p-4'>
      <div className='mb-1 flex items-center justify-between gap-2'>
        <div className='flex items-center gap-2.5'>
          <span aria-hidden className='bg-primary block h-3 w-[3px]' />
          <div className='text-foreground text-[13px] font-semibold'>
            <Trans i18nKey={'开奖状态'} />
          </div>
        </div>
        {props.availableBoxes > 0 ? (
          <span className='codego-stat-label text-primary'>
            {props.availableBoxes} <Trans i18nKey={'待开'} />
          </span>
        ) : null}
      </div>
      <div>
        <StatRow label={i18n.t('通用额度')} value={formatQuota(props.quota)} />
        <StatRow
          label={i18n.t('待开盲盒')}
          value={String(props.availableBoxes)}
          tone={props.availableBoxes > 0 ? 'text-primary' : undefined}
        />
        <StatRow label={i18n.t('待结算')} value={String(props.pendingBoxes)} />
      </div>
    </div>
  )
}

function StatRow(props: { label: string; value: string; tone?: string }) {
  return (
    <div className='border-border/60 flex items-baseline justify-between gap-3 border-b py-2.5 last:border-b-0'>
      <span className='codego-stat-label'>{props.label}</span>
      <span
        className={cn(
          'text-sm font-semibold tabular-nums',
          props.tone || 'text-foreground'
        )}
      >
        {props.value}
      </span>
    </div>
  )
}

function SettlementCard() {
  return (
    <div className='app-subtle-panel p-4'>
      <div className='mb-3 flex items-center gap-2.5'>
        <span aria-hidden className='bg-primary block h-3 w-[3px]' />
        <div className='text-foreground text-[13px] font-semibold'>
          <Trans i18nKey={'结算'} />
        </div>
      </div>
      <div className='codego-empty justify-start gap-2 py-1 text-left'>
        <span className='codego-stat-label'>
          <Trans i18nKey={'统一额度'} />
        </span>
        <span className='text-muted-foreground font-sans text-xs tracking-normal normal-case'>
          <Trans i18nKey={'永久有效'} />
        </span>
      </div>
      <div className='codego-empty mt-2 justify-start gap-2 py-1 text-left'>
        <span className='codego-stat-label'>
          <Trans i18nKey={'道具'} />
        </span>
        <span className='text-muted-foreground font-sans text-xs tracking-normal normal-case'>
          <Trans i18nKey={'自动生效或手动启用'} />
        </span>
      </div>
    </div>
  )
}
