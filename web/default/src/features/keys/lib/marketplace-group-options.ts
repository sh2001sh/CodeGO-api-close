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
import type { ApiKeyGroupOption } from '../components/api-key-group-combobox'

export interface KeyGroupOptionResponse {
  value: string
  label: string
  category: 'marketplace' | 'marketplace_auto' | 'marketplace_pool'
  multiplier?: number
  subscription_enabled?: boolean
  subscription_multiplier?: number
  mapping_status?: ApiKeyGroupOption['mappingStatus']
  models: string[]
  member_count?: number
  success_rate?: number
  avg_ttft_ms?: number
  request_count?: number
}

export function toApiKeyGroupOptions(
  options: KeyGroupOptionResponse[],
  t: (key: string) => string = (key) => key
): ApiKeyGroupOption[] {
  return options.map((option) => ({
    value: option.value,
    label: option.label,
    desc:
      option.category === 'marketplace'
        ? undefined
        : `${option.member_count ?? 0} ${t('Groups')} · ${option.models.length} ${t('Models')}`,
    ratio: option.category === 'marketplace' ? option.multiplier : '动态',
    subscriptionEnabled: option.subscription_enabled,
    subscriptionRatio: option.subscription_multiplier,
    mappingStatus: option.mapping_status,
    category: option.category,
    models: option.models,
    successRate: option.success_rate,
    avgTTFTMs: option.avg_ttft_ms,
    requestCount: option.request_count,
  }))
}
