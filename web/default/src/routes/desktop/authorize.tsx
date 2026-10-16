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
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { PublicLayout } from '@/components/layout'
import {
  approveDesktopAuthSession,
  getDesktopAuthSession,
  rejectDesktopAuthSession,
} from '@/features/profile/api'
import '@/features/setup/setup.css'
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
    <PublicLayout showMainContainer={false}>
      <main id='content' tabIndex={-1} className='market-desktop px-5 sm:px-8'>
        <div className='mx-auto flex w-full max-w-2xl flex-col gap-6'>
          <header className='market-desktop-heading'>
            <h1 className='text-3xl font-semibold tracking-tight'>
              {t('Approve desktop access')}
            </h1>
            <span className='market-desktop-product'>
              <Monitor className='size-4' aria-hidden='true' />
              Code Go Desktop
            </span>
          </header>

          <Card className='market-desktop-card'>
            <CardHeader className='space-y-2'>
              <CardTitle className='text-base'>
                {t(viewModel.titleKey)}
              </CardTitle>
            </CardHeader>
            <CardContent className='space-y-4'>
              {sessionQuery.isLoading ? (
                <div
                  role='status'
                  className='market-desktop-notice market-desktop-loading'
                >
                  <Loader2 className='h-4 w-4 animate-spin' />
                  {t('Loading desktop session...')}
                </div>
              ) : sessionQuery.isError ? (
                <div
                  role='alert'
                  className='market-desktop-notice market-desktop-danger'
                >
                  <ShieldAlert className='mt-0.5 h-4 w-4 shrink-0' />
                  <span>
                    {viewModel.errorMessage ||
                      t('Desktop authorization session not found')}
                  </span>
                </div>
              ) : session ? (
                <>
                  <dl className='market-desktop-identity'>
                    <div>
                      <dt>{t('Device')}</dt>
                      <dd>{session.device_name}</dd>
                    </div>
                    <div>
                      <dt>{t('Code')}</dt>
                      <dd className='market-desktop-code'>
                        {session.user_code}
                      </dd>
                    </div>
                    <div>
                      <dt>{t('Platform')}</dt>
                      <dd>{t(viewModel.platformLabel)}</dd>
                    </div>
                    <div>
                      <dt>{t('Status')}</dt>
                      <dd>
                        <span
                          className='market-desktop-status'
                          data-state={status}
                        >
                          {session.status}
                        </span>
                      </dd>
                    </div>
                    <div>
                      <dt>{t('Requested at')}</dt>
                      <dd>{formatTimestampToDate(session.created_at)}</dd>
                    </div>
                    <div>
                      <dt>{t('Expires at')}</dt>
                      <dd>{formatTimestampToDate(session.expires_at)}</dd>
                    </div>
                  </dl>

                  <section className='market-desktop-permissions'>
                    <div className='text-sm font-medium'>
                      {t('This desktop will be allowed to')}
                    </div>
                    <ul className='text-muted-foreground mt-3 space-y-2 text-sm'>
                      {(session.permissions || []).map((permission) => (
                        <li key={permission} className='flex items-start gap-2'>
                          <CheckCircle2
                            className='market-desktop-permission-icon mt-0.5 h-4 w-4 shrink-0'
                            aria-hidden='true'
                          />
                          <span>{t(permission)}</span>
                        </li>
                      ))}
                    </ul>
                  </section>

                  {viewModel.noticeKey && viewModel.noticeTone === 'success' ? (
                    <div
                      role='status'
                      className='market-desktop-notice market-desktop-success'
                    >
                      <CheckCircle2 className='mt-0.5 h-4 w-4 shrink-0' />
                      <span>{t(viewModel.noticeKey)}</span>
                    </div>
                  ) : viewModel.noticeKey &&
                    viewModel.noticeTone === 'danger' ? (
                    <div
                      role='status'
                      className='market-desktop-notice market-desktop-danger'
                    >
                      {status === 'rejected' ? (
                        <ShieldX className='mt-0.5 h-4 w-4 shrink-0' />
                      ) : (
                        <ShieldAlert className='mt-0.5 h-4 w-4 shrink-0' />
                      )}
                      <span>{t(viewModel.noticeKey)}</span>
                    </div>
                  ) : viewModel.canReview ? (
                    <div className='market-desktop-actions'>
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
      </main>
    </PublicLayout>
  )
}
