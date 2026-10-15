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
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Textarea } from '@/components/ui/textarea'
import { StatusBadge } from '@/components/status-badge'

const specificChannelsExample = JSON.stringify(
  {
    mode: 'force',
    all_channels: false,
    channel_ids: [1, 2],
    channel_types: [1],
    model_patterns: ['^gpt-4o.*$', '^gpt-5.*$'],
  },
  null,
  2
)

const disableExample = JSON.stringify(
  {
    mode: 'disabled',
    all_channels: true,
  },
  null,
  2
)

type ProtocolBridgePolicyEditorProps = {
  title: string
  description: string
  value: string
  onChange: (value: string) => void
  onFormat: () => void
}

export function ProtocolBridgePolicyEditor({
  title,
  description,
  value,
  onChange,
  onFormat,
}: ProtocolBridgePolicyEditorProps) {
  const { t } = useTranslation()

  return (
    <FormItem>
      <div className='flex flex-wrap items-center gap-2'>
        <FormLabel>{title}</FormLabel>
        <StatusBadge label={t('Advanced')} variant='neutral' copyable={false} />
      </div>
      <FormControl>
        <Textarea
          rows={10}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          placeholder={`${t('Force example:')}\n${specificChannelsExample}\n\n${t('Disable example:')}\n${disableExample}`}
        />
      </FormControl>
      <FormDescription>
        {description}{' '}
        {t(
          'Use mode force or disabled with channel_ids, channel_types, and optional model_patterns. Empty value is saved as {} and keeps automatic routing.'
        )}
      </FormDescription>
      <div className='flex flex-wrap gap-2'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => onChange(specificChannelsExample)}
        >
          {t('Fill force example')}
        </Button>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={() => onChange(disableExample)}
        >
          {t('Fill disable example')}
        </Button>
        <Button type='button' variant='outline' size='sm' onClick={onFormat}>
          {t('Format JSON')}
        </Button>
      </div>
      <FormMessage />
    </FormItem>
  )
}
