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
import { Circle } from 'lucide-react'
import { useTranslation } from 'react-i18next'

type FormDirtyIndicatorProps = {
  isDirty: boolean
  message?: string
}

/**
 * Visual indicator that the form has unsaved changes
 *
 * @example
 * ```tsx
 * <FormDirtyIndicator isDirty={form.formState.isDirty} />
 * ```
 */
export function FormDirtyIndicator({
  isDirty,
  message,
}: FormDirtyIndicatorProps) {
  const { t } = useTranslation()
  if (!isDirty) return null

  return (
    <div className='settings-dirty-status' role='status'>
      <Circle aria-hidden='true' className='size-2 fill-current' />
      <span>{message ?? t('Unsaved changes')}</span>
    </div>
  )
}
