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

export interface GroupBuyItem {
  id: number
  plan_id: number
  plan_name: string
  plan_price: number
  currency: string
  base_quota_usd: number
  current_count: number
  target_count: number
  bonus_at_2: number
  bonus_at_3: number
  bonus_at_5: number
  expires_at: number
  initiator_id: number
  initiator_avatar?: string
  status: string
  joined?: boolean
}

export interface GroupBuyListResponse {
  data: GroupBuyItem[]
  total: number
}
