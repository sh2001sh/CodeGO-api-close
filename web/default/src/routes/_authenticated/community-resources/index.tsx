import { createFileRoute } from '@tanstack/react-router'
import { CommunityResourcesPage } from '@/features/community-resources'

export const Route = createFileRoute('/_authenticated/community-resources/')({
  component: CommunityResourcesPage,
})
