import type { Order } from '../../lib/commerce'
import { useNotificationTranslation } from '../notifications/messages'

export function LegacyCashBoxReview({ order }: { order: Order }) {
  const { nt } = useNotificationTranslation()
  if (
    order.purchase_type !== 'legacy_cash_box_review' ||
    order.fulfillment_state !== 'requires_review'
  )
    return null
  return (
    <div className="section">
      <strong>{nt('legacyCashBoxReview')}</strong>
      <p className="muted">{nt('legacyCashBoxReviewBody', { order: order.trade_no })}</p>
    </div>
  )
}
