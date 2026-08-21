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
import { useQuery } from '@tanstack/react-query'
import axios from 'axios'
import { useAuthStore } from '@/stores/auth-store'
import { getSidebarGroupStatus } from './api'

export function useSidebarGroupStatus() {
  const userId = useAuthStore((state) => state.auth.user?.id ?? 0)

  return useQuery({
    queryKey: ['sidebar-group-status', userId],
    queryFn: getSidebarGroupStatus,
    enabled: userId > 0,
    staleTime: 60 * 1000,
    refetchInterval: 60 * 1000,
    refetchIntervalInBackground: false,
    refetchOnWindowFocus: true,
    retry: (failureCount, error) => {
      if (failureCount >= 2) return false
      if (!axios.isAxiosError(error)) return true
      const status = error.response?.status
      // Authentication and validation errors should be shown immediately;
      // retry only network failures and transient upstream/control errors.
      return status == null || status === 408 || status === 429 || status >= 500
    },
    retryDelay: (attemptIndex) => Math.min(500 * 2 ** attemptIndex, 2000),
  })
}
