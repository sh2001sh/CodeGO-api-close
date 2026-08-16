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
    description: '渠道声明的模型与实际调用结果一致',
    icon: CheckCircle2,
    tone: 'text-emerald-600',
  },
  {
    status: 'failed' as const,
    label: '不通过',
    description: '发现模型冒充、替换或与声明明显不一致',
    icon: XCircle,
    tone: 'text-rose-600',
  },
  {
    status: 'questionable' as const,
    label: '存疑',
    description: '现有调用样本不足，暂时无法确认一致性',
    icon: CircleHelp,
    tone: 'text-amber-600',
  },
]

export function ChannelFeedbackSummary({ group }: { group: MarketplaceGroup }) {
  const { t } = useTranslation()
  const summary = group.channel_feedback
  return (
    <div
      className='flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] tabular-nums'
      aria-label={t('模型一致性反馈统计')}
    >
      <span className='text-muted-foreground'>{t('模型一致性')}:</span>
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
        title={disabled ? reason : t('反馈模型一致性')}
        onClick={() => setOpen(true)}
      >
        <MessageSquare />
        {t('反馈')}
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className='max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('模型一致性反馈')}</DialogTitle>
            <DialogDescription className='space-y-1'>
              <span className='text-foreground block font-medium'>
                {group.system_display_name}
              </span>
              <span className='block'>
                {t(
                  '请根据实际调用结果，判断该渠道声明的模型是否与实际返回模型一致。反馈针对整个渠道，不需要逐个模型提交。'
                )}
              </span>
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-3 py-1'>
            <div className='bg-muted/35 rounded-md px-3 py-2.5'>
              <ChannelFeedbackSummary group={group} />
            </div>
            <div className='grid gap-2'>
              {feedbackOptions.map(
                ({ status, label, description, icon: Icon, tone }) => (
                  <button
                    key={status}
                    type='button'
                    aria-pressed={selected === status}
                    onClick={() => setSelected(status)}
                    className={cn(
                      'border-border hover:bg-muted flex min-h-16 items-start gap-3 rounded-md border px-3 py-3 text-left focus-visible:ring-2 focus-visible:outline-none',
                      selected === status && 'border-primary bg-primary/5'
                    )}
                  >
                    <Icon className={cn('mt-0.5 size-4 shrink-0', tone)} />
                    <span className='min-w-0 flex-1'>
                      <span className='block text-sm font-medium'>
                        {t(label)}
                      </span>
                      <span className='text-muted-foreground mt-0.5 block text-xs leading-5'>
                        {t(description)}
                      </span>
                    </span>
                    <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
                      {group.channel_feedback[status]} {t('人')}
                    </span>
                  </button>
                )
              )}
            </div>
            <p className='text-muted-foreground text-xs leading-5'>
              {t(
                '每位用户对同一渠道只保留一条反馈；再次提交会更新你之前的选择。'
              )}
            </p>
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
