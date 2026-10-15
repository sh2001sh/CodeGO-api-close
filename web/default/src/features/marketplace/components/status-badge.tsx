import {
  AlertTriangle,
  CheckCircle2,
  Clock3,
  PauseCircle,
  Search,
  XCircle,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { statusLabels } from '../lib/format'
import type { MarketplaceStatus } from '../types'

const statusMeta = {
  draft: { icon: Clock3, className: 'text-muted-foreground' },
  verifying: { icon: Search, className: 'text-info' },
  pending_review: { icon: Clock3, className: 'text-warning' },
  active: { icon: CheckCircle2, className: 'text-success' },
  degraded: { icon: AlertTriangle, className: 'text-warning' },
  suspended: { icon: PauseCircle, className: 'text-destructive' },
  disabled: { icon: XCircle, className: 'text-muted-foreground' },
} satisfies Record<
  MarketplaceStatus,
  { icon: typeof Clock3; className: string }
>

export function MarketplaceStatusBadge(props: { status: MarketplaceStatus }) {
  const meta = statusMeta[props.status]
  const Icon = meta.icon
  return (
    <Badge
      variant='outline'
      className={cn('gap-1 font-medium', meta.className)}
    >
      <Icon className='size-3.5' aria-hidden='true' />
      {statusLabels[props.status]}
    </Badge>
  )
}
