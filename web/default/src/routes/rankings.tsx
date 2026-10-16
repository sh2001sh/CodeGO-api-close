import z from 'zod'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { getModuleAccess, getModuleAccessFromStatus } from '@/lib/nav-modules'
import { statusQueryOptions } from '@/hooks/use-status'
import { RankingsPage } from '@/features/rankings'

const rankingsSearchSchema = z.object({
  sort: z.enum(['score', 'success_rate', 'requests']).catch('score'),
  page: z.coerce.number().int().min(1).max(10000).catch(1),
  hours: z.coerce
    .number()
    .pipe(z.union([z.literal(24), z.literal(168), z.literal(720)]))
    .catch(24),
  metric: z.enum(['request_count', 'input_tokens']).catch('request_count'),
})

export const Route = createFileRoute('/rankings')({
  validateSearch: rankingsSearchSchema,
  beforeLoad: async ({ context, location }) => {
    const status = await context.queryClient
      .ensureQueryData(statusQueryOptions())
      .catch(() => null)
    const access = status
      ? getModuleAccessFromStatus(
          status as unknown as Record<string, unknown>,
          'rankings'
        )
      : getModuleAccess('rankings')
    if (!access.enabled) throw redirect({ to: '/' })
    if (access.requireAuth && !useAuthStore.getState().auth.user) {
      throw redirect({
        to: '/sign-in',
        search: { redirect: location.href },
      })
    }
  },
  component: RankingsPage,
})
