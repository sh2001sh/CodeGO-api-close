import { ArrowRight, Gift, History, Info, Wallet } from 'lucide-react'
import { motion, useReducedMotion, type Variants } from 'motion/react'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import type { BlindBoxProp, BlindBoxRecord, BlindBoxStatistics } from '../types'
import { formatBlindBoxTimestamp } from './blind-box-dialogs'
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
  remainingQuota: number
  claudeQuota: number
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
          remainingQuota={props.remainingQuota}
          claudeQuota={props.claudeQuota}
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
        <div className='app-subtle-panel p-4'>
          <div className='flex items-start gap-3'>
            <div className='bg-muted text-muted-foreground flex size-9 shrink-0 items-center justify-center rounded-lg'>
              <History className='size-4' />
            </div>
            <div className='min-w-0 flex-1'>
              <div className='text-foreground text-sm font-semibold'>
                开奖历史
              </div>
              {props.records[0] ? (
                <div className='text-muted-foreground mt-1 text-xs leading-5'>
                  最近获得
                  <span className='text-foreground mx-1 font-medium'>
                    {props.records[0].reward_title}
                  </span>
                  · {formatBlindBoxTimestamp(props.records[0].create_time)}
                </div>
              ) : (
                <div className='text-muted-foreground mt-1 text-xs leading-5'>
                  最近 30 天还没有抽取记录
                </div>
              )}
            </div>
          </div>
          <Button
            type='button'
            variant='outline'
            className='mt-4 w-full justify-between'
            onClick={props.onOpenHistory}
          >
            查看最近 30 天记录
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
    <div className='app-subtle-panel p-4'>
      <div className='flex items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <Gift className='text-muted-foreground size-4' aria-hidden='true' />
          <div className='text-foreground text-sm font-semibold'>我的道具</div>
        </div>
        {usable.length > 0 ? (
          <span className='border-primary/30 bg-primary/10 text-primary rounded-full border px-2 py-0.5 text-[10px] font-semibold tabular-nums'>
            可用 {usable.length}
          </span>
        ) : null}
      </div>

      {preview.length > 0 ? (
        <ul className='mt-3 space-y-1.5'>
          {preview.map((prop) => (
            <li
              key={prop.id}
              className='border-border/70 bg-background/60 flex items-center justify-between gap-2 rounded-lg border px-3 py-2'
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
        <p className='text-muted-foreground mt-2 text-xs leading-5'>
          还没有道具，抽中折扣卡或倍率卡后会显示在这里。
        </p>
      )}

      <Button
        type='button'
        variant='outline'
        className='mt-3 w-full justify-between'
        onClick={props.onOpenProps}
      >
        管理全部道具
        <ArrowRight className='size-4' />
      </Button>
    </div>
  )
}

function AssetBoard(props: {
  remainingQuota: number
  claudeQuota: number
  availableBoxes: number
  pendingBoxes: number
}) {
  return (
    <div className='app-subtle-panel p-4'>
      <div className='mb-3 flex items-center gap-2'>
        <Wallet className='text-muted-foreground size-4' />
        <div className='text-foreground text-sm font-semibold'>开奖状态</div>
      </div>
      <div className='grid grid-cols-2 gap-2.5'>
        <Tile label='通用额度' value={formatQuota(props.claudeQuota)} />
        <Tile
          label='官方 GPT 专属额度'
          value={formatQuota(props.remainingQuota)}
        />
        <Tile
          label='待开盲盒'
          value={String(props.availableBoxes)}
          highlight={props.availableBoxes > 0}
        />
        <Tile label='待结算' value={String(props.pendingBoxes)} />
      </div>
    </div>
  )
}

function Tile(props: { label: string; value: string; highlight?: boolean }) {
  return (
    <div
      className={cn(
        'rounded-xl border px-3 py-2.5',
        props.highlight
          ? 'border-primary/25 bg-primary/6'
          : 'border-border/70 bg-background/60'
      )}
    >
      <div className='text-muted-foreground text-[11px] font-medium'>
        {props.label}
      </div>
      <div className='text-foreground mt-1 text-base font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}

function SettlementCard() {
  return (
    <div className='app-subtle-panel p-4'>
      <div className='mb-3 flex items-center gap-2'>
        <Info className='text-muted-foreground size-4' />
        <div className='text-foreground text-sm font-semibold'>到账说明</div>
      </div>
      <div className='space-y-2 text-xs leading-5'>
        <div className='border-border/70 bg-background/60 rounded-xl border px-3 py-2.5'>
          通用额度直接进入通用额度钱包，永久有效。
        </div>
        <div className='border-border/70 bg-background/60 rounded-xl border px-3 py-2.5'>
          官方 GPT 专属额度直接进入对应钱包，永久有效。
        </div>
        <div className='border-border/70 bg-background/60 rounded-xl border px-3 py-2.5'>
          道具会在本页展示并按规则自动生效或手动启用。
        </div>
      </div>
    </div>
  )
}
