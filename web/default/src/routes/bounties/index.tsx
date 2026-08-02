import { z } from 'zod'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { AuthenticatedLayout, PublicLayout } from '@/components/layout'
import { BountyMarket } from '@/features/bounties'
import type { BountyTaskStatus } from '@/features/bounties/types'

const bountySearchSchema = z.object({
  scope: z
    .enum(['all', 'mine_published', 'mine_assigned', 'mine_disputes'])
    .optional(),
  keyword: z.string().optional(),
  wallet_type: z.enum(['all', 'wallet', 'claude_wallet']).optional(),
  status: z
    .enum([
      'all',
      'available',
      'active',
      'ending_soon',
      'completed',
      'draft',
      'published',
      'selecting',
      'assigned',
      'in_progress',
      'waiting_for_publisher',
      'publisher_replied',
      'submitted',
      'reviewing',
      'changes_requested',
      'expired',
      'cancelled',
      'disputed',
      'resolved',
      'suspended',
    ])
    .optional(),
  sort: z.enum(['latest', 'reward_desc', 'deadline_asc']).optional(),
  tag: z.string().optional(),
  min_reward: z.coerce.number().positive().optional(),
  max_reward: z.coerce.number().positive().optional(),
  page: z.coerce.number().int().positive().optional(),
})

export const Route = createFileRoute('/bounties/')({
  validateSearch: bountySearchSchema,
  component: BountiesRoute,
})

function BountiesRoute() {
  const search = Route.useSearch()
  const navigate = useNavigate({ from: Route.fullPath })
  const user = useAuthStore((state) => state.auth.user)
  const content = (
    <BountyMarket
      search={
        search as {
          scope?: 'all' | 'mine_published' | 'mine_assigned' | 'mine_disputes'
          keyword?: string
          wallet_type?: 'all' | 'wallet' | 'claude_wallet'
          status?:
            | 'all'
            | 'available'
            | 'active'
            | 'ending_soon'
            | BountyTaskStatus
          sort?: 'latest' | 'reward_desc' | 'deadline_asc'
          tag?: string
          min_reward?: number
          max_reward?: number
          page?: number
        }
      }
      onSearchChange={(next) => {
        void navigate({ search: (previous) => ({ ...previous, ...next }) })
      }}
    />
  )

  if (user) {
    return <AuthenticatedLayout>{content}</AuthenticatedLayout>
  }

  return (
    <PublicLayout showMainContainer={false}>
      <div className='pt-16'>{content}</div>
    </PublicLayout>
  )
}
