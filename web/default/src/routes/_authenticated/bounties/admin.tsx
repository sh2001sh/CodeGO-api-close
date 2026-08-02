import { createFileRoute } from '@tanstack/react-router'
import { BountyAdminPage } from '@/features/bounties/components/bounty-admin-page'

export const Route = createFileRoute('/_authenticated/bounties/admin')({
  component: BountyAdminPage,
})
