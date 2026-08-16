import { useState } from 'react'
import {
  CheckCircle2,
  CircleHelp,
  Loader2,
  MessageSquare,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useMarketplaceChannelFeedback } from '../hooks'
import type { MarketplaceGroup, ModelConsistencyStatus } from '../types'

const feedbackOptions = [
  {
    status: 'passed' as const,
    label: '通过',
    icon: CheckCircle2,
    tone: 'text-emerald-600',
  },
  {
    status: 'failed' as const,
    label: '不通过',
    icon: XCircle,
    tone: 'text-rose-600',
  },
  {
    status: 'questionable' as const,
    label: '存疑',
    icon: CircleHelp,
    tone: 'text-amber-600',
  },
]

export function ChannelFeedbackSummary({ group }: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const summary = group.channel_feedback
  return (
    <div
      className='mt-2 flex flex-wrap gap-x-3 gap-y-1 text-[11px] tabular-nums'
      aria-label={t('用户反馈统计')}
    >
      <span className='text-emerald-600'>
        {t('通过')} {summary.passed}
      </span>
      <span className='text-rose-600'>
        {t('不通过')} {summary.failed}
      </span>
      <span className='text-amber-600'>
        {t('存疑')} {summary.questionable}
      </span>
    </div>
  )
}

export function ChannelFeedbackButton({ group }: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const mutation = useMarketplaceChannelFeedback()
  const [open, setOpen] = useState(false)
  const [selected, setSelected] = useState<
    Exclude<ModelConsistencyStatus, ''> | ''
  >(group.channel_feedback.viewer_status || '')
  const submit = async () => {
    if (!selected) return
    try {
      await mutation.mutateAsync({ groupId: group.id, status: selected })
      toast.success(t('渠道反馈已提交'))
      setOpen(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('反馈提交失败'))
    }
  }
  const disabled = !group.can_submit_channel_feedback
  const reason =
    group.channel_feedback_permission === 'owner'
      ? t('渠道主不能评价自己的渠道')
      : t('登录后可提交渠道反馈')
  return (
    <>
      <Button
        type='button'
        variant='outline'
        size='sm'
        disabled={disabled}
        title={disabled ? reason : t('反馈此渠道')}
        onClick={() => setOpen(true)}
      >
        <MessageSquare />
        {t('反馈')}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('反馈渠道')}</DialogTitle>
            <DialogDescription>{group.system_display_name}</DialogDescription>
          </DialogHeader>
          <div className='grid gap-2 py-2'>
            {feedbackOptions.map(({ status, label, icon: Icon, tone }) => (
              <button
                key={status}
                type='button'
                onClick={() => setSelected(status)}
                className={cn(
                  'border-border hover:bg-muted flex min-h-12 items-center gap-3 rounded-md border px-3 text-left text-sm focus-visible:ring-2 focus-visible:outline-none',
                  selected === status && 'border-primary bg-primary/5'
                )}
              >
                <Icon className={cn('size-4', tone)} />
                <span className='flex-1'>{t(label)}</span>
                <span className='text-muted-foreground text-xs tabular-nums'>
                  {group.channel_feedback[status]} {t('人')}
                </span>
              </button>
            ))}
          </div>
          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              onClick={() => setOpen(false)}
            >
              {t('取消')}
            </Button>
            <Button
              type='button'
              disabled={!selected || mutation.isPending}
              onClick={() => void submit()}
            >
              {mutation.isPending && <Loader2 className='animate-spin' />}
              {t('提交反馈')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
