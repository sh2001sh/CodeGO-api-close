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
import type { TFunction } from 'i18next'
import { parseQuotaFromDollars } from '@/lib/format'
import type { User } from '../types'
import { UserQuotaDialog } from './user-quota-dialog'

interface UserQuotaDialogsProps {
  currentRow?: User
  quotaDialogOpen: boolean
  claudeQuotaDialogOpen: boolean
  currentQuotaRaw: number
  currentClaudeQuotaRaw: number
  t: TFunction
  onQuotaOpenChange: (open: boolean) => void
  onClaudeQuotaOpenChange: (open: boolean) => void
  onSuccess: () => void
}

export function UserQuotaDialogs(props: UserQuotaDialogsProps) {
  if (!props.currentRow) {
    return null
  }

  return (
    <>
      <UserQuotaDialog
        open={props.quotaDialogOpen}
        onOpenChange={props.onQuotaOpenChange}
        userId={props.currentRow.id}
        currentQuota={parseQuotaFromDollars(props.currentQuotaRaw || 0)}
        onSuccess={props.onSuccess}
      />
      <UserQuotaDialog
        open={props.claudeQuotaDialogOpen}
        onOpenChange={props.onClaudeQuotaOpenChange}
        userId={props.currentRow.id}
        currentQuota={parseQuotaFromDollars(props.currentClaudeQuotaRaw || 0)}
        action='add_claude_quota'
        title={props.t('Adjust Claude Quota')}
        currentLabel={props.t('当前通用额度')}
        onSuccess={props.onSuccess}
      />
    </>
  )
}
