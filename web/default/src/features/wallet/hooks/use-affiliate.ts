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
import { useCallback, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { getAffiliateRewardsOverview } from '../api'
import { generateAffiliateLink } from '../lib'

const AFFILIATE_OVERVIEW_QUERY_KEY = [
  'wallet',
  'affiliate',
  'overview',
] as const

export function useAffiliate() {
  const { copyToClipboard } = useCopyToClipboard()

  const overviewQuery = useQuery({
    queryKey: AFFILIATE_OVERVIEW_QUERY_KEY,
    queryFn: async () => {
      const response = await getAffiliateRewardsOverview()
      return response.success ? (response.data ?? null) : null
    },
    staleTime: 60 * 1000,
  })

  const affiliateCode = overviewQuery.data?.affiliate_code ?? ''
  const affiliateLink = useMemo(
    () => (affiliateCode ? generateAffiliateLink(affiliateCode) : ''),
    [affiliateCode]
  )

  const copyAffiliateLink = useCallback(async () => {
    if (!affiliateLink) return
    await copyToClipboard(affiliateLink)
  }, [affiliateLink, copyToClipboard])

  const copyAffiliateCode = useCallback(async () => {
    if (!affiliateCode) return
    await copyToClipboard(affiliateCode)
  }, [affiliateCode, copyToClipboard])

  return {
    overview: overviewQuery.data,
    affiliateCode,
    affiliateLink,
    loading: overviewQuery.isLoading,
    copyAffiliateCode,
    copyAffiliateLink,
    refetch: overviewQuery.refetch,
  }
}
