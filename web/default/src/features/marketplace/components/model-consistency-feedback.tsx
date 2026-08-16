import { CheckCircle2, CircleHelp, Loader2, XCircle } from 'lucide-react'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { useMarketplaceModelFeedback } from '../hooks'
import type { MarketplaceGroup, ModelConsistencyStatus } from '../types'

const options = [
  {
    status: 'passed' as const,
    label: '通过',
    icon: CheckCircle2,
    count: 'passed' as const,
  },
  {
    status: 'failed' as const,
    label: '不通过',
    icon: XCircle,
    count: 'failed' as const,
  },
  {
    status: 'questionable' as const,
    label: '存疑',
    icon: CircleHelp,
    count: 'questionable' as const,
  },
]

export function ModelConsistencyFeedback(props: { group: MarketplaceGroup }) {
  const mutation = useMarketplaceModelFeedback()
  const summaries = props.group.model_consistency_feedback ?? []
  if (summaries.length === 0) return null

  const submit = async (
    model: string,
    status: Exclude<ModelConsistencyStatus, ''>
  ) => {
    try {
      await mutation.mutateAsync({ groupId: props.group.id, model, status })
      toast.success(`已提交 ${model} 的模型一致性反馈`)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '反馈提交失败')
    }
  }

  return (
    <section className='border-border mt-3 overflow-hidden rounded-lg border'>
      <div className='bg-muted/30 px-3 py-2'>
        <div className='text-xs font-medium'>用户模型一致性反馈</div>
        <p className='text-muted-foreground mt-0.5 text-[11px] leading-5'>
          反馈用于辅助判断模型与标称是否一致，不替代渠道连通性检测。
        </p>
      </div>
      <div className='divide-border max-h-72 divide-y overflow-y-auto'>
        {summaries.map((summary) => (
          <div key={summary.model} className='px-3 py-3'>
            <div className='flex items-center justify-between gap-3'>
              <span
                className='min-w-0 truncate text-xs font-medium'
                title={summary.model}
              >
                {summary.model}
              </span>
              <span className='text-muted-foreground shrink-0 text-[11px] tabular-nums'>
                {summary.total} 份反馈
              </span>
            </div>
            <div className='mt-2 grid grid-cols-3 gap-1.5'>
              {options.map((option) => {
                const selected = summary.viewer_status === option.status
                const Icon = option.icon
                return (
                  <Button
                    key={option.status}
                    type='button'
                    size='sm'
                    variant={selected ? 'default' : 'outline'}
                    className={cn(
                      'min-w-0 px-2 text-xs',
                      selected && 'pointer-events-none'
                    )}
                    disabled={
                      !props.group.can_submit_model_feedback ||
                      mutation.isPending
                    }
                    onClick={() => void submit(summary.model, option.status)}
                  >
                    {mutation.isPending &&
                    mutation.variables?.model === summary.model ? (
                      <Loader2 className='size-3.5 animate-spin' />
                    ) : (
                      <Icon className='size-3.5' />
                    )}
                    <span className='truncate'>
                      {option.label} {summary[option.count]}
                    </span>
                  </Button>
                )
              })}
            </div>
          </div>
        ))}
      </div>
      {props.group.model_feedback_permission !== 'allowed' ? (
        <p className='text-muted-foreground border-border border-t px-3 py-2 text-[11px]'>
          {props.group.model_feedback_permission === 'owner'
            ? '渠道主不能评价自己渠道的模型一致性。'
            : '登录后可提交模型一致性反馈。'}
        </p>
      ) : null}
    </section>
  )
}
