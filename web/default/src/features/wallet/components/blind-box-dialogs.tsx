import { useState } from 'react'
import { Trophy } from 'lucide-react'
import { useReducedMotion } from 'motion/react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { BlindBoxRecord, BlindBoxTier } from '../types'
import {
  formatBlindBoxTimestamp,
  summarizeOpenResult,
  type PrizeDialogState,
} from './blind-box-dialog-data'
import { PrizeRevealHeader, PrizeRevealList } from './blind-box-prize-reveal'
import { BlindBoxReel } from './blind-box-reel'
import { tiersToReelItems, type ReelItem } from './blind-box-reel-data'

export function BlindBoxPrizeDialog(props: {
  state: PrizeDialogState
  tiers?: BlindBoxTier[]
  onOpenChange: (open: boolean) => void
  onUseReward?: (record: BlindBoxRecord) => void
}) {
  return (
    <Dialog open={props.state.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-2xl flex-col overflow-hidden p-0 sm:max-h-[calc(100dvh-2rem)]'>
        <BlindBoxPrizeDialogContent {...props} />
      </DialogContent>
    </Dialog>
  )
}

function BlindBoxPrizeDialogContent(
  props: Parameters<typeof BlindBoxPrizeDialog>[0]
) {
  const reduced = Boolean(useReducedMotion())
  const [phase, setPhase] = useState<'reel' | 'reveal'>(
    reduced ? 'reveal' : 'reel'
  )
  const winner = winnerReelItem(props.state.records[0])
  const pool = tiersToReelItems(props.tiers || [])

  return (
    <>
      <DialogHeader className='shrink-0 border-b px-4 py-4 sm:px-5'>
        <DialogTitle className='flex items-center gap-2 text-base'>
          <Trophy className='text-primary size-5' />
          {phase === 'reel' ? '正在开启' : '抽奖结果'}
        </DialogTitle>
      </DialogHeader>

      {phase === 'reel' ? (
        <div className='bg-card py-10'>
          <BlindBoxReel
            pool={pool}
            winner={winner}
            reduced={reduced}
            onComplete={() => setPhase('reveal')}
          />
        </div>
      ) : (
        <>
          <div className='min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-4 sm:px-5 sm:py-5'>
            <div className='space-y-4'>
              <PrizeRevealHeader
                summary={summarizeOpenResult(props.state.records)}
                openCount={props.state.openCount}
                records={props.state.records}
              />

              <PrizeRevealList
                records={props.state.records}
                onUseReward={props.onUseReward}
                formatTimestamp={formatBlindBoxTimestamp}
              />
            </div>
          </div>
          <div className='bg-background shrink-0 border-t px-4 py-3 sm:px-5'>
            <Button
              className='w-full'
              onClick={() => props.onOpenChange(false)}
            >
              确定
            </Button>
          </div>
        </>
      )}
    </>
  )
}

function winnerReelItem(record?: BlindBoxRecord): ReelItem {
  if (!record) {
    return { key: 'empty', label: '统一额度奖励' }
  }
  const legendary = record.is_pity || record.reward_type === 'subscription'
  const rare = !legendary && Number(record.reward_usd || 0) >= 2
  return {
    key: `record-${record.id}`,
    label: record.reward_title || '统一额度奖励',
    tag: legendary ? '稀有' : rare ? '精品' : undefined,
    strong: Boolean(legendary),
  }
}
