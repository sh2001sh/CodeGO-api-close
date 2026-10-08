import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'

export type NotificationCategory = 'all' | 'market' | 'billing' | 'review' | 'rewards' | 'system'

export const notificationSummaryOptions = () => ({
  ...resourceOptions('notification-summary', (signal) =>
    api.GET('/api/notifications/summary', { signal }).then(unwrap),
  ),
  staleTime: Infinity,
  refetchOnMount: false,
})

export const notificationsOptions = (
  category: NotificationCategory,
  unread: boolean,
  page: number,
) =>
  resourceOptions(
    'notifications',
    (signal) =>
      api
        .GET('/api/notifications', {
          signal,
          params: { query: { category, unread, page, page_size: 20 } },
        })
        .then(unwrap),
    [category, unread, page],
  )
