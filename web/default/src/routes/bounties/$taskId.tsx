import { createFileRoute } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { AuthenticatedLayout, PublicLayout } from '@/components/layout'
import { BountyDetail } from '@/features/bounties/components/bounty-detail'

export const Route = createFileRoute('/bounties/$taskId')({
  component: BountyDetailRoute,
})

function BountyDetailRoute() {
  const { taskId } = Route.useParams()
  const user = useAuthStore((state) => state.auth.user)
  const content = (
    <main className='min-h-svh px-4 pt-6 pb-8 sm:px-6'>
      <BountyDetail taskId={taskId} />
    </main>
  )

  if (user) {
    return <AuthenticatedLayout>{content}</AuthenticatedLayout>
  }

  return (
    <PublicLayout showMainContainer={false}>
      <main className='min-h-svh px-4 pt-24 pb-8 sm:px-6'>
        <BountyDetail taskId={taskId} />
      </main>
    </PublicLayout>
  )
}
