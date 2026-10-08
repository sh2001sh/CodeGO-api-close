import { credits } from '../../lib/format'
import type { TranslationParameters } from '../../lib/i18n'
import { multiplierText } from './helpers'
import type { NotificationMessage } from './messages'

export function notificationPresentation(
  item: { kind: string; data?: Record<string, unknown> | null },
  noReason: string,
): { title: NotificationMessage; body: NotificationMessage; parameters?: TranslationParameters } {
  const data = item.data ?? {}
  if (item.kind === 'legacy_cash_box_payment_review')
    return {
      title: 'legacyCashBoxReview',
      body: 'legacyCashBoxReviewBody',
      parameters: { order: typeof data.trade_no === 'string' ? data.trade_no : '—' },
    }
  if (item.kind === 'channel_review')
    return {
      title: data.status === 'approved' ? 'channelApproved' : 'channelRejected',
      body: data.status === 'approved' ? 'channelApprovedBody' : 'channelRejectedBody',
      parameters: {
        channel: typeof data.channel_id === 'string' ? data.channel_id : '—',
        reason: typeof data.reason === 'string' && data.reason.trim() ? data.reason : noReason,
      },
    }
  if (item.kind === 'bargain_requested')
    return {
      title: 'bargainRequested',
      body: 'bargainRequestedBody',
      parameters: {
        channel: typeof data.channel_id === 'string' ? data.channel_id : '—',
        proposed: multiplierText(data.proposed_ppm),
      },
    }
  if (item.kind === 'multiplier_changed')
    return {
      title: 'changed',
      body: data.cleared === true ? 'clearedBody' : 'changedBody',
      parameters: {
        channel: typeof data.channel_id === 'string' ? data.channel_id : '—',
        previous: multiplierText(data.previous_multiplier_ppm),
        current: multiplierText(data.multiplier_ppm),
      },
    }
  if (item.kind === 'lucky_reward')
    return {
      title: 'reward',
      body: 'rewardBody',
      parameters: {
        amount: credits(
          typeof data.final_reward_credits === 'string' ? data.final_reward_credits : undefined,
        ),
      },
    }
  if (['order_paid', 'order_refunded', 'order_failed'].includes(item.kind))
    return {
      title:
        item.kind === 'order_paid'
          ? 'orderPaid'
          : item.kind === 'order_refunded'
            ? 'orderRefunded'
            : 'orderFailed',
      body: 'orderBody',
      parameters: { order: typeof data.order_id === 'string' ? data.order_id : '—' },
    }
  if (item.kind === 'shop_review')
    return data.status === 'approved'
      ? {
          title: 'shopApproved',
          body: 'shopApprovedBody',
        }
      : {
          title: 'shopRejected',
          body: 'shopRejectedBody',
          parameters: {
            reason: typeof data.reason === 'string' && data.reason.trim() ? data.reason : noReason,
          },
        }
  if (item.kind === 'bargain_resolved')
    return data.status === 'accepted'
      ? {
          title: 'bargainAccepted',
          body: 'bargainAcceptedBody',
        }
      : {
          title: 'bargainRejected',
          body: 'bargainRejectedBody',
          parameters: {
            reason: typeof data.note === 'string' && data.note.trim() ? data.note : noReason,
          },
        }
  return { title: 'unknown', body: 'unknownBody' }
}
