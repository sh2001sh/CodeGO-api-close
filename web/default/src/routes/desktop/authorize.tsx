/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { z } from 'zod'
import { useMutation, useQuery } from '@tanstack/react-query'
import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import {
  CheckCircle2,
  ExternalLink,
  Loader2,
  Monitor,
  ShieldAlert,
  ShieldX,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { getSelf } from '@/lib/api'
import { formatTimestampToDate } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  approveDesktopAuthSession,
  getDesktopAuthSession,
  rejectDesktopAuthSession,
} from '@/features/profile/api'
import { getDesktopAuthorizationStatus } from './-authorize-state'
import {
  buildDesktopAuthorizeViewModel,
  type DesktopAuthorizeSession,
} from './-authorize-view'

const searchSchema = z.object({
  session_id: z.string().min(1),
  code: z.string().min(1),
})

export const Route = createFileRoute('/desktop/authorize')({
  validateSearch: searchSchema,
  beforeLoad: async ({ location }) => {
    const { auth } = useAuthStore.getState()

    if (!auth.user) {
      throw redirect({
        to: '/sign-in',
        search: { redirect: location.href },
      })
    }

    const res = await getSelf().catch(() => null)
    if (!res?.success || !res.data) {
      auth.reset()
      throw redirect({
        to: '/sign-in',
        search: { redirect: location.href },
      })
    }
    auth.setUser(res.data)
  },
  component: DesktopAuthorizeRoute,
})

function DesktopAuthorizeRoute() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { session_id: sessionId, code } = Route.useSearch()

  const sessionQuery = useQuery({
    queryKey: ['desktop-authorize', sessionId, code],
    queryFn: async () => {
      const res = await getDesktopAuthSession({ sessionId, code })
      if (!res.success) {
        throw new Error(
          res.message || 'Desktop authorization session not found'
        )
      }
      return res.data as DesktopAuthorizeSession
    },
  })

  const approveMutation = useMutation({
    mutationFn: async () => {
      const res = await approveDesktopAuthSession(sessionId)
      if (!res.success) {
        throw new Error(res.message || 'Failed to approve desktop session')
      }
      return res.data
    },
    onSuccess: () => {
      toast.success(t('Desktop device approved'))
      void sessionQuery.refetch()
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to approve desktop session'))
    },
  })

  const rejectMutation = useMutation({
    mutationFn: async () => {
      const res = await rejectDesktopAuthSession(sessionId)
      if (!res.success) {
        throw new Error(res.message || 'Failed to reject desktop session')
      }
      return res.data
    },
    onSuccess: () => {
      toast.success(t('Desktop request rejected'))
      void sessionQuery.refetch()
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to reject desktop session'))
    },
  })

  const session = sessionQuery.data
  const viewModel = buildDesktopAuthorizeViewModel({
    error: sessionQuery.error instanceof Error ? sessionQuery.error : null,
    isLoading: sessionQuery.isLoading,
    session,
  })
  const status = getDesktopAuthorizationStatus(session?.status)

  return (
    <div className='bg-background min-h-screen px-4 py-10 sm:px-6'>
      <div className='mx-auto flex w-full max-w-2xl flex-col gap-6'>
        <div className='space-y-3'>
          <Badge className='w-fit gap-2 rounded-full bg-orange-500/10 px-3 py-1 text-orange-700 hover:bg-orange-500/10'>
            <Monitor className='h-3.5 w-3.5' />
            Code Go Desktop
          </Badge>
          <div className='space-y-2'>
            <h1 className='text-3xl font-semibold tracking-tight'>
              {t('Approve desktop access')}
            </h1>
            <p className='text-muted-foreground max-w-xl text-sm leading-6'>
              {t(
                'Review this desktop request before allowing it to access your Code Go account.'
              )}
            </p>
          </div>
        </div>

        <Card className='border-border/70 bg-card/95 shadow-sm'>
          <CardHeader className='space-y-2'>
            <CardTitle className='text-base'>{t(viewModel.titleKey)}</CardTitle>
          </CardHeader>
          <CardContent className='space-y-4'>
            {sessionQuery.isLoading ? (
              <div className='text-muted-foreground flex items-center gap-3 rounded-lg border px-4 py-4 text-sm'>
                <Loader2 className='h-4 w-4 animate-spin' />
                {t('Loading desktop session...')}
              </div>
            ) : sessionQuery.isError ? (
              <div className='border-destructive/20 bg-destructive/5 text-destructive flex items-start gap-3 rounded-lg border px-4 py-4 text-sm'>
                <ShieldAlert className='mt-0.5 h-4 w-4 shrink-0' />
                <span>
                  {viewModel.errorMessage ||
                    t('Desktop authorization session not found')}
                </span>
              </div>
            ) : session ? (
              <>
                <div className='bg-muted/20 grid gap-3 rounded-lg border p-4 sm:grid-cols-2'>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Device')}
                    </div>
                    <div className='mt-1 text-sm font-medium'>
                      {session.device_name}
                    </div>
                  </div>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Code')}
                    </div>
                    <div className='mt-1 font-mono text-sm font-medium tracking-[0.18em]'>
                      {session.user_code}
                    </div>
                  </div>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Platform')}
                    </div>
                    <div className='mt-1 text-sm font-medium'>
                      {t(viewModel.platformLabel)}
                    </div>
                  </div>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Status')}
                    </div>
                    <div className='mt-1 text-sm font-medium'>
                      {session.status}
                    </div>
                  </div>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Requested at')}
                    </div>
                    <div className='mt-1 text-sm font-medium'>
                      {formatTimestampToDate(session.created_at)}
                    </div>
                  </div>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Expires at')}
                    </div>
                    <div className='mt-1 text-sm font-medium'>
                      {formatTimestampToDate(session.expires_at)}
                    </div>
                  </div>
                </div>

                <div className='bg-muted/20 rounded-lg border p-4'>
                  <div className='text-sm font-medium'>
                    {t('This desktop will be allowed to')}
                  </div>
                  <ul className='text-muted-foreground mt-3 space-y-2 text-sm'>
                    {(session.permissions || []).map((permission) => (
                      <li key={permission} className='flex items-start gap-2'>
                        <CheckCircle2 className='mt-0.5 h-4 w-4 shrink-0 text-emerald-600' />
                        <span>{t(permission)}</span>
                      </li>
                    ))}
                  </ul>
                </div>

                {viewModel.noticeKey && viewModel.noticeTone === 'success' ? (
                  <div className='flex items-start gap-3 rounded-lg border border-emerald-500/20 bg-emerald-500/5 px-4 py-4 text-sm text-emerald-700 dark:text-emerald-300'>
                    <CheckCircle2 className='mt-0.5 h-4 w-4 shrink-0' />
                    <span>{t(viewModel.noticeKey)}</span>
                  </div>
                ) : viewModel.noticeKey && viewModel.noticeTone === 'danger' ? (
                  <div className='border-destructive/20 bg-destructive/5 text-destructive flex items-start gap-3 rounded-lg border px-4 py-4 text-sm'>
                    {status === 'rejected' ? (
                      <ShieldX className='mt-0.5 h-4 w-4 shrink-0' />
                    ) : (
                      <ShieldAlert className='mt-0.5 h-4 w-4 shrink-0' />
                    )}
                    <span>{t(viewModel.noticeKey)}</span>
                  </div>
                ) : viewModel.canReview ? (
                  <div className='flex flex-wrap gap-3'>
                    <Button
                      onClick={() => approveMutation.mutate()}
                      disabled={
                        approveMutation.isPending || rejectMutation.isPending
                      }
                      className='gap-2'
                    >
                      {approveMutation.isPending ? (
                        <Loader2 className='h-4 w-4 animate-spin' />
                      ) : (
                        <CheckCircle2 className='h-4 w-4' />
                      )}
                      {t(viewModel.primaryActionKey || 'Approve desktop')}
                    </Button>
                    <Button
                      variant='outline'
                      onClick={() => rejectMutation.mutate()}
                      disabled={
                        approveMutation.isPending || rejectMutation.isPending
                      }
                      className='gap-2'
                    >
                      {rejectMutation.isPending ? (
                        <Loader2 className='h-4 w-4 animate-spin' />
                      ) : (
                        <ShieldX className='h-4 w-4' />
                      )}
                      {t(viewModel.secondaryActionKey || 'Reject desktop')}
                    </Button>
                    <Button
                      variant='outline'
                      onClick={() => navigate({ to: '/profile' })}
                      className='gap-2'
                    >
                      <ExternalLink className='h-4 w-4' />
                      {t('Open profile')}
                    </Button>
                  </div>
                ) : null}
              </>
            ) : null}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
