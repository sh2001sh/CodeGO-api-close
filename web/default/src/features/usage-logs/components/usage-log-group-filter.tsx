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
import type { KeyboardEventHandler } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { ComboboxInput } from '@/components/ui/combobox-input'
import { Input } from '@/components/ui/input'
import { getUsageLogGroups } from '../api'

interface UsageLogGroupFilterProps {
  value: string
  onValueChange: (value: string) => void
  onValueCommit: (value: string) => void
  onKeyDown: KeyboardEventHandler<HTMLInputElement>
  sensitiveVisible: boolean
  isAdmin: boolean
  className?: string
}

function publicGroupLabel(label: string, publicID?: string): string {
  if (!publicID) return label
  const prefix = `${publicID}-`
  return label.startsWith(prefix) ? label.slice(prefix.length) : label
}

/** Searchable usage-log group filter backed by groups visible to the viewer. */
export function UsageLogGroupFilter(props: UsageLogGroupFilterProps) {
  const { t } = useTranslation()
  const groupsQuery = useQuery({
    queryKey: ['usage-log-groups', props.isAdmin ? 'admin' : 'self'],
    queryFn: () => getUsageLogGroups(props.isAdmin),
    staleTime: 5 * 60 * 1000,
    retry: 1,
  })
  const options = (groupsQuery.data ?? []).map((group) => ({
    value: group.value,
    label: publicGroupLabel(group.label, group.public_id),
    ...(group.public_id ? { meta: `ID ${group.public_id}` } : {}),
  }))

  return (
    <div className={cn(props.className)}>
      <label className='sr-only' htmlFor='usage-log-group-filter'>
        {t('Group')}
      </label>
      {props.sensitiveVisible ? (
        <ComboboxInput
          id='usage-log-group-filter'
          options={options}
          value={props.value}
          onValueChange={props.onValueChange}
          onValueCommit={props.onValueCommit}
          placeholder={
            groupsQuery.isLoading ? t('Loading...') : t('Search groups...')
          }
          emptyText={
            groupsQuery.isError ? t('Failed to load') : t('No option found.')
          }
          allowCustomValue
          className='w-full'
        />
      ) : (
        <Input
          id='usage-log-group-filter'
          placeholder={t('Group')}
          type='password'
          value={props.value}
          onChange={(event) => props.onValueChange(event.target.value)}
          onKeyDown={props.onKeyDown}
          className='w-full'
        />
      )}
    </div>
  )
}
