import i18n from '@/i18n/config'
import { Trophy } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { useTranslation, Trans } from 'react-i18next'
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

export function BlindBoxPrizeDialog(props: {
  state: PrizeDialogState
  tiers?: BlindBoxTier[]
  onOpenChange: (open: boolean) => void
  onUseReward?: (record: BlindBoxRecord) => void
}) {
  return (
    <Dialog open={props.state.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='commerce-dialog commerce-workspace flex max-h-[calc(100dvh-1rem)] w-[calc(100vw-1rem)] max-w-2xl flex-col overflow-hidden p-0 sm:max-h-[calc(100dvh-2rem)]'>
        <BlindBoxPrizeDialogContent {...props} />
      </DialogContent>
    </Dialog>
  )
}

function BlindBoxPrizeDialogContent(
  props: Parameters<typeof BlindBoxPrizeDialog>[0]
) {
  useTranslation()
  const reduced = Boolean(useReducedMotion())

  return (
    <>
      <DialogHeader className='shrink-0 border-b px-4 py-4 sm:px-5'>
        <DialogTitle className='flex items-center gap-2 text-base'>
          <Trophy className='text-primary size-5' />
          {i18n.t('抽奖结果')}
        </DialogTitle>
      </DialogHeader>

      <motion.div
        initial={{ opacity: reduced ? 1 : 0.7 }}
        animate={{ opacity: 1 }}
        transition={{ duration: reduced ? 0 : 0.28 }}
        className='min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 py-4 sm:px-5 sm:py-5'
      >
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
      </motion.div>
      <div className='bg-background shrink-0 border-t px-4 py-3 sm:px-5'>
        <Button className='w-full' onClick={() => props.onOpenChange(false)}>
          <Trans i18nKey={'确定'} />
        </Button>
      </div>
    </>
  )
}
