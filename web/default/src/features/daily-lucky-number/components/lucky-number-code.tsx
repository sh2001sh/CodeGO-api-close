import { Check, Copy } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { normalizeLuckyNumber } from '../lib'

export function LuckyNumberCode(props: {
  cardCode?: string
  luckySuffix?: string
  compact?: boolean
  className?: string
}) {
  const { copiedText, copyToClipboard } = useCopyToClipboard()
  const code = props.cardCode?.trim() || ''
  const suffix = normalizeLuckyNumber(props.luckySuffix)
  const copied = code !== '' && copiedText === code

  if (!code) {
    return <span className='text-muted-foreground text-xs'>号码待生成</span>
  }

  return (
    <span className={cn('inline-flex min-w-0 items-center gap-1', props.className)}>
      <span
        className={cn(
          'text-foreground min-w-0 truncate font-mono text-sm font-semibold tabular-nums',
          props.compact && 'text-xs'
        )}
      >
        {code}
      </span>
      {!props.compact && suffix ? (
        <span className='text-muted-foreground font-mono text-xs tabular-nums'>
          · {suffix}
        </span>
      ) : null}
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='ghost'
                size='icon-xs'
                aria-label={copied ? '已复制' : '复制号码'}
                onClick={() => void copyToClipboard(code)}
              />
            }
          >
            {copied ? <Check className='text-success' /> : <Copy />}
          </TooltipTrigger>
          <TooltipContent>{copied ? '已复制' : '复制号码'}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
    </span>
  )
}
