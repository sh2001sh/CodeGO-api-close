import type { MarketplaceOwnerUsageItem } from '../types'

export type OwnerUsageSort = 'requests' | 'amount'

export function rankOwnerUsers(
  items: readonly MarketplaceOwnerUsageItem[],
  channelID: string,
  sort: OwnerUsageSort
) {
  return items
    .filter((item) => item.channel_id === channelID)
    .sort((left, right) => {
      const countDifference = right.request_count - left.request_count
      if (sort === 'amount') {
        const amountDifference =
          right.total_settlement_gross_amount -
          left.total_settlement_gross_amount
        if (amountDifference) return amountDifference
      }
      return (
        countDifference ||
        left.user_id.localeCompare(right.user_id, 'en', { numeric: true })
      )
    })
}
