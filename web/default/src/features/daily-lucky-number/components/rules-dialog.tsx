import { BookOpen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { normalizeLuckyNumberRules } from '../lib'
import type { LuckyNumberRules } from '../types'
import { RulesDialogContent } from './rules-dialog-content'

export function DailyLuckyRulesDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  rules?: Partial<LuckyNumberRules> | null
  timezone?: string
  drawHour?: number
  drawMinute?: number
}) {
  const { t } = useTranslation()
  const rules = normalizeLuckyNumberRules(props.rules)
  const timezone = props.timezone || 'Asia/Shanghai'
  const drawTime = `${String(props.drawHour ?? 20).padStart(2, '0')}:${String(props.drawMinute ?? 0).padStart(2, '0')}`
  const tiers = [
    {
      name: t('Lite'),
      label: t('Lite membership card'),
      multiplier: rules.multiplier_lite,
      tone: 'text-slate-600 dark:text-slate-300',
    },
    {
      name: t('Standard'),
      label: t('Standard membership card'),
      multiplier: rules.multiplier_standard,
      tone: 'text-blue-600 dark:text-blue-300',
    },
    {
      name: t('Pro'),
      label: t('Pro membership card'),
      multiplier: rules.multiplier_pro,
      tone: 'text-violet-600 dark:text-violet-300',
    },
    {
      name: t('Ultra'),
      label: t('Ultra membership card'),
      multiplier: rules.multiplier_ultra,
      tone: 'text-amber-700 dark:text-amber-300',
    },
  ]

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='flex h-[90vh] max-h-[calc(100dvh-2rem)] min-h-0 max-w-4xl flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl'>
        <div className='border-border flex shrink-0 items-start gap-3 border-b px-5 py-5 pr-14 text-left sm:px-7'>
          <span className='bg-primary/10 text-primary flex size-10 shrink-0 items-center justify-center rounded-xl'>
            <BookOpen className='size-5' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <DialogTitle className='text-lg font-semibold'>
              {t('Daily Lucky Number Rules')}
            </DialogTitle>
            <DialogDescription className='mt-2 text-left leading-6'>
              {t(
                'This guide opens automatically the first time you enter the page. After closing it, click "View full rules" at the top of the page to open it again.'
              )}
            </DialogDescription>
          </div>
        </div>

        <ScrollArea className='min-h-0 flex-1'>
          <RulesDialogContent
            rules={rules}
            tiers={tiers}
            drawTime={drawTime}
            timezone={timezone}
          />
        </ScrollArea>

        <div className='border-border bg-muted/20 flex shrink-0 items-center justify-between gap-3 border-t px-5 py-3 sm:px-7'>
          <span className='text-muted-foreground hidden text-xs leading-5 sm:block'>
            {t(
              'Rules are based on the valid state captured at draw time and the current configuration shown on this page.'
            )}
          </span>
          <DialogClose render={<Button size='sm' />}>{t('Got it')}</DialogClose>
        </div>
      </DialogContent>
    </Dialog>
  )
}
