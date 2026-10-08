import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { Dialog } from '@base-ui/react/dialog'
import { create } from 'zustand'
import { Bell, Check, CheckCheck, RefreshCw, X } from 'lucide-react'
import { api } from '../../lib/api'
import { sessionOptions } from '../../lib/queries'
import { date } from '../../lib/format'
import { Button, IconButton, EmptyState, ErrorMessage, Loading } from '../ui'
import {
  notificationAction,
  unreadEvent,
  observedThroughID,
  exactCount,
} from '../../features/notifications/helpers'
import { useNotificationTranslation } from '../../features/notifications/messages'
import { notificationPresentation } from '../../features/notifications/presentation'
import {
  notificationsOptions,
  notificationSummaryOptions,
  type NotificationCategory,
} from '../../features/notifications/queries'

type NotificationConnection = 'connecting' | 'connected' | 'reconnecting'
// The full page observes the topbar's connection instead of opening a second stream.
const useNotificationConnection = create<{
  connection: NotificationConnection
  retry?: () => void
}>(() => ({
  connection: 'connecting',
}))
const setConnection = (connection: NotificationConnection) =>
  useNotificationConnection.setState({ connection })

export function NotificationCenter() {
  const session = useQuery({ ...sessionOptions(), throwOnError: false })
  return session.data ? <SignedInNotifications key={String(session.data.id)} /> : null
}

export function SignedInNotifications(props: { fullPage?: boolean }) {
  const { t, nt } = useNotificationTranslation()
  const client = useQueryClient()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [category, setCategory] = useState<NotificationCategory>('all')
  const [unread, setUnread] = useState(false)
  const [page, setPage] = useState(1)
  const connection = useNotificationConnection((state) => state.connection)
  const reconnect = useNotificationConnection((state) => state.retry)
  const summary = useQuery({ ...notificationSummaryOptions(), retry: false })
  const notices = useQuery({
    ...notificationsOptions(category, unread, page),
    enabled: props.fullPage || open,
  })
  const count = exactCount(summary.data?.unread_count)
  const total = exactCount(notices.data?.total) ?? 0n

  useEffect(() => {
    if (props.fullPage) return
    let stream: EventSource | undefined
    let retryTimer: ReturnType<typeof setTimeout> | undefined
    let retries = 0
    let stopped = false
    let syncing = false
    let generation = 0
    const clearRetry = () => {
      if (retryTimer !== undefined) clearTimeout(retryTimer)
      retryTimer = undefined
    }
    const scheduleRetry = () => {
      if (stopped || document.hidden || retryTimer !== undefined || retries >= 5) return
      retryTimer = setTimeout(
        () => {
          retryTimer = undefined
          void reconcile(false)
        },
        1000 * 2 ** retries++,
      )
    }
    const connect = () => {
      if (stopped || document.hidden || stream) return
      setConnection('connecting')
      const current = new EventSource('/api/notifications/events')
      stream = current
      current.onopen = () => {
        if (stream === current) setConnection('connected')
      }
      current.onerror = () => {
        if (stream !== current) return
        current.close()
        stream = undefined
        setConnection('reconnecting')
        scheduleRetry()
      }
      const update = (event: MessageEvent<string>) => {
        if (stream !== current || stopped) return
        const next = unreadEvent(event.data)
        if (next === undefined) {
          current.onerror?.(new Event('error'))
          return
        }
        // A pending older HTTP snapshot must not overwrite this live event.
        void client.cancelQueries({ queryKey: ['notification-summary'] }).then(() => {
          if (stream !== current || stopped) return
          retries = 0
          setConnection('connected')
          client.setQueryData(notificationSummaryOptions().queryKey, (previous) =>
            previous ? { ...previous, unread_count: next } : undefined,
          )
          void client.invalidateQueries({ queryKey: ['notifications'] })
        })
      }
      // The initial count also refreshes an open list after a disconnected period.
      current.addEventListener('unread_count', update)
      current.addEventListener('invalidate', update)
    }
    const reconcile = async (resetRetry = true) => {
      if (stopped || document.hidden) return
      if (resetRetry) {
        clearRetry()
        retries = 0
      }
      if (syncing) return
      syncing = true
      const attempt = generation
      try {
        // JSON transport renews expired sessions. Establish its snapshot before
        // opening cookie-only EventSource, which cannot refresh a session itself.
        await client.fetchQuery({ ...notificationSummaryOptions(), staleTime: 0, retry: false })
        if (stopped || document.hidden || attempt !== generation) return
        void client.invalidateQueries({ queryKey: ['notifications'] }, { cancelRefetch: false })
        connect()
      } catch {
        if (stopped || document.hidden || attempt !== generation || stream) return
        setConnection('reconnecting')
        scheduleRetry()
      } finally {
        if (attempt === generation) syncing = false
      }
    }
    const visibility = () => {
      if (document.hidden) {
        generation++
        syncing = false
        clearRetry()
        stream?.close()
        stream = undefined
      } else void reconcile()
    }
    const focus = () => void reconcile()
    useNotificationConnection.setState({ retry: focus })
    void reconcile()
    document.addEventListener('visibilitychange', visibility)
    window.addEventListener('focus', focus)
    return () => {
      stopped = true
      generation++
      clearRetry()
      stream?.close()
      document.removeEventListener('visibilitychange', visibility)
      window.removeEventListener('focus', focus)
      useNotificationConnection.setState({ retry: undefined, connection: 'connecting' })
    }
  }, [client, props.fullPage])

  useEffect(() => {
    if (open)
      void client.refetchQueries(
        { queryKey: ['notification-summary'], type: 'active' },
        { cancelRefetch: false },
      )
  }, [client, open])

  const changed = () => {
    void client.invalidateQueries({ queryKey: ['notification-summary'] })
    void client.invalidateQueries({ queryKey: ['notifications'] })
  }
  const markRead = useMutation({
    mutationFn: ({ id, read }: { id: string; read: boolean }) =>
      api.POST('/api/notifications/{id}/read', { params: { path: { id } }, body: { read } }),
    onSuccess: changed,
  })
  const markAll = useMutation({
    mutationFn: (body: { through_id: string; category: NotificationCategory }) =>
      api.POST('/api/notifications/read-all', { body }),
    onSuccess: () => {
      setPage(1)
      changed()
    },
  })
  const refresh = () => {
    if (reconnect) reconnect()
    else {
      void summary.refetch()
      void notices.refetch()
    }
  }
  const categories: readonly NotificationCategory[] = [
    'all',
    'market',
    'billing',
    'review',
    'rewards',
    'system',
  ]
  const items = notices.data?.items ?? []
  const mutationPending = markRead.isPending || markAll.isPending
  const throughID = observedThroughID(notices.data?.latest_id)

  useEffect(() => {
    if (notices.data && page > 1 && items.length === 0) setPage(page - 1)
  }, [notices.data, page, items.length])

  const content = (
    <>
      <div className="drawer-header">
        <div className="notification-heading">
          {props.fullPage ? (
            <h1>{nt('center')}</h1>
          ) : (
            <Dialog.Title className="dialog-title">{nt('center')}</Dialog.Title>
          )}
          {count !== undefined && (
            <span className="subtle" aria-live="polite">
              {nt('unread')} · {count}
            </span>
          )}
        </div>
        <div className="notification-header-actions">
          <IconButton
            label={t('刷新')}
            onClick={refresh}
            disabled={notices.isFetching || summary.isFetching}
          >
            <RefreshCw size={17} aria-hidden />
          </IconButton>
          {!props.fullPage && (
            <Dialog.Close render={<IconButton label={t('关闭')} />}>
              <X size={18} aria-hidden />
            </Dialog.Close>
          )}
        </div>
      </div>
      <div className="notification-controls">
        <div className="notification-tabs" role="group" aria-label={nt('center')}>
          <button
            type="button"
            aria-pressed={!unread}
            onClick={() => {
              setUnread(false)
              setPage(1)
            }}
          >
            {nt('all')}
          </button>
          <button
            type="button"
            aria-pressed={unread}
            onClick={() => {
              setUnread(true)
              setPage(1)
            }}
          >
            {nt('unread')}
          </button>
        </div>
        <select
          className="notification-category"
          aria-label={nt('category')}
          value={category}
          onChange={(event) => {
            const selected = categories.find((value) => value === event.target.value)
            if (selected) {
              setCategory(selected)
              setPage(1)
            }
          }}
        >
          {categories.map((value) => (
            <option key={value} value={value}>
              {nt(value)}
            </option>
          ))}
        </select>
        <Button
          variant="ghost"
          size="sm"
          disabled={mutationPending || !count || !throughID || notices.isError || notices.isPending}
          onClick={() => throughID && markAll.mutate({ through_id: throughID, category })}
          aria-label={nt(category === 'all' ? 'readAll' : 'readCategory')}
        >
          <CheckCheck size={16} aria-hidden />
          {nt('readAll')}
        </Button>
      </div>
      <div className="notification-body">
        {connection === 'reconnecting' && (
          <div className="notification-reconnect" role="status">
            {nt('reconnecting')}
            <Button variant="ghost" size="sm" onClick={refresh}>
              {t('重试')}
            </Button>
          </div>
        )}
        <ErrorMessage error={markRead.error ?? markAll.error} />
        {notices.isPending ? (
          <Loading rows={5} />
        ) : notices.isError ? (
          <div className="notification-error" role="alert">
            <p>{nt('loadFailed')}</p>
            <Button variant="secondary" onClick={() => void notices.refetch()}>
              {t('重试')}
            </Button>
          </div>
        ) : items.length === 0 ? (
          <EmptyState
            icon={<Bell size={22} />}
            title={nt(unread ? 'emptyUnread' : 'empty')}
            description={unread ? undefined : nt('emptyDetail')}
          />
        ) : (
          <ol className="notification-list" aria-busy={notices.isFetching}>
            {items.map((item) => {
              const action = notificationAction(item.action_url)
              const copy = notificationPresentation(item, nt('noReason'))
              const title = nt(copy.title, copy.parameters)
              const body = nt(copy.body, copy.parameters)
              return (
                <li
                  key={item.id}
                  className="notification-item"
                  data-unread={!item.read_at || undefined}
                >
                  <div className="notification-meta">
                    <span>
                      {nt(categories.find((value) => value === item.category) ?? 'center')}
                    </span>
                    <time dateTime={item.created_at}>{date(item.created_at)}</time>
                  </div>
                  <h3>
                    {!item.read_at && (
                      <span className="notification-dot" aria-label={nt('unread')} />
                    )}
                    {title}
                  </h3>
                  <p>{body}</p>
                  <div className="notification-item-actions">
                    {action && (
                      <a
                        href={action}
                        onClick={(event) => {
                          if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
                            return
                          event.preventDefault()
                          const go = () => {
                            setOpen(false)
                            void navigate({ href: action })
                          }
                          if (item.read_at) go()
                          else markRead.mutate({ id: item.id, read: true }, { onSuccess: go })
                        }}
                      >
                        {nt('details')}
                      </a>
                    )}
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={mutationPending}
                      onClick={() => markRead.mutate({ id: item.id, read: !item.read_at })}
                    >
                      {item.read_at ? (
                        <Bell size={15} aria-hidden />
                      ) : (
                        <Check size={15} aria-hidden />
                      )}
                      {nt(item.read_at ? 'markUnread' : 'read')}
                    </Button>
                  </div>
                </li>
              )
            })}
          </ol>
        )}
      </div>
      {notices.data && total > BigInt(notices.data.page_size) && (
        <div className="drawer-footer notification-pagination">
          <Button
            variant="secondary"
            size="sm"
            disabled={page === 1 || notices.isFetching}
            onClick={() => setPage(page - 1)}
          >
            {t('上一页')}
          </Button>
          <span>
            {page} /{' '}
            {(
              (total + BigInt(notices.data.page_size) - 1n) /
              BigInt(notices.data.page_size)
            ).toString()}
          </span>
          <Button
            variant="secondary"
            size="sm"
            disabled={BigInt(page) * BigInt(notices.data.page_size) >= total || notices.isFetching}
            onClick={() => setPage(page + 1)}
          >
            {t('下一页')}
          </Button>
        </div>
      )}
      {!props.fullPage && (
        <div className="drawer-footer">
          <Link
            to="/notifications"
            className="button button-secondary"
            onClick={() => setOpen(false)}
          >
            {nt('viewAll')}
          </Link>
        </div>
      )}
    </>
  )
  if (props.fullPage) return <section className="page notification-page">{content}</section>
  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Trigger
        render={
          <IconButton
            className="notification-trigger"
            label={count ? nt('unreadLabel', { count: count.toString() }) : nt('center')}
          />
        }
      >
        <Bell size={18} aria-hidden />
        {count !== undefined && count > 0n && (
          <span className="notification-count" aria-hidden>
            {count > 99n ? '99+' : count.toString()}
          </span>
        )}
        {summary.isError && (
          <span className="notification-unavailable" aria-hidden>
            !
          </span>
        )}
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Backdrop className="overlay-backdrop" />
        <Dialog.Popup className="drawer-popup notification-drawer" data-side="right">
          {content}
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
