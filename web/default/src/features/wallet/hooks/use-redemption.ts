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
import { useState, useCallback } from 'react'
import i18next from 'i18next'
import { toast } from 'sonner'
import { getSelf } from '@/lib/api'
import { formatQuota } from '@/lib/format'
import { redeemTopupCode } from '../api'

// ============================================================================
// Redemption Hook
// ============================================================================

export function useRedemption() {
  const [redeeming, setRedeeming] = useState(false)

  const redeemCode = useCallback(async (code: string): Promise<boolean> => {
    if (!code || code.trim() === '') {
      toast.error(i18next.t('Please enter a redemption code'))
      return false
    }

    try {
      setRedeeming(true)
      const response = await redeemTopupCode({ key: code })

      if (response.success && response.data) {
        const result = response.data
        if (result.redeem_type === 'subscription') {
          const planLabel =
            result.plan_title ||
            (result.plan_id
              ? i18next.t('Plan #{{id}}', { id: result.plan_id })
              : i18next.t('Subscription'))
          toast.success(
            i18next.t(
              'Redemption successful! Subscription activated: {{plan}}',
              {
                plan: planLabel,
              }
            )
          )
          if (typeof window !== 'undefined') {
            window.dispatchEvent(new Event('subscription:changed'))
          }
        } else if (result.redeem_type === 'blind_box') {
          toast.success(
            i18next.t(
              'Redemption successful! Opened {{count}} blind box(es) automatically',
              {
                count:
                  result.blind_box_open_count || result.blind_box_quantity || 0,
              }
            )
          )
          if (typeof window !== 'undefined') {
            window.dispatchEvent(
              new CustomEvent('blind-box:changed', {
                detail: {
                  records: result.blind_box_records || [],
                  openCount:
                    result.blind_box_open_count ||
                    result.blind_box_quantity ||
                    0,
                },
              })
            )
          }
        } else {
          const quotaText = formatQuota(result.quota || 0)
          toast.success(
            i18next.t('Redemption successful! Added universal quota: {{quota}}', {
              quota: quotaText,
            })
          )
        }
        await getSelf()
        return true
      }

      toast.error(response.message || i18next.t('Redemption failed'))
      return false
    } catch (_error) {
      toast.error(i18next.t('Redemption failed'))
      return false
    } finally {
      setRedeeming(false)
    }
  }, [])

  return {
    redeeming,
    redeemCode,
  }
}
