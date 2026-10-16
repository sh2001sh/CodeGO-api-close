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
import { useId, useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { OAUTH_PRESETS, type CustomOAuthFormValues } from '../types'

type PresetSelectorProps = {
  form: UseFormReturn<CustomOAuthFormValues>
}

export function PresetSelector(props: PresetSelectorProps) {
  const { t } = useTranslation()
  const [selectedPreset, setSelectedPreset] = useState<string>('')
  const [baseUrl, setBaseUrl] = useState<string>('')
  const presetId = useId()
  const baseUrlId = useId()
  const currentPreset = OAUTH_PRESETS.find((p) => p.key === selectedPreset)

  const handlePresetChange = (presetKey: string) => {
    setSelectedPreset(presetKey)
    const preset = OAUTH_PRESETS.find((p) => p.key === presetKey)
    if (!preset) return

    // Auto-fill name, slug, icon, and field mappings immediately
    props.form.setValue('name', preset.name, { shouldDirty: true })
    props.form.setValue('slug', presetKey.toLowerCase().replace(/\s+/g, '-'), {
      shouldDirty: true,
    })
    props.form.setValue('icon', preset.icon, { shouldDirty: true })
    props.form.setValue('scopes', preset.scopes, { shouldDirty: true })
    props.form.setValue('user_id_field', preset.user_id_field, {
      shouldDirty: true,
    })
    props.form.setValue('username_field', preset.username_field, {
      shouldDirty: true,
    })
    props.form.setValue('display_name_field', preset.display_name_field, {
      shouldDirty: true,
    })
    props.form.setValue('email_field', preset.email_field, {
      shouldDirty: true,
    })

    props.form.setValue('well_known', preset.well_known ?? '', {
      shouldDirty: true,
    })
    props.form.setValue('auth_style', preset.auth_style ?? 0, {
      shouldDirty: true,
    })
    props.form.setValue('access_policy', preset.access_policy ?? '', {
      shouldDirty: true,
    })
    props.form.setValue('access_denied_message', '', { shouldDirty: true })
    applyEndpoints(preset, baseUrl)
  }

  const handleBaseUrlChange = (url: string) => {
    setBaseUrl(url)
    if (!selectedPreset) return

    const preset = OAUTH_PRESETS.find((p) => p.key === selectedPreset)
    if (!preset) return

    applyEndpoints(preset, url)
  }

  const applyEndpoints = (
    preset: (typeof OAUTH_PRESETS)[number],
    url: string
  ) => {
    const cleanUrl = preset.needsBaseUrl ? url.trim().replace(/\/+$/, '') : ''
    const resolveEndpoint = (endpoint: string) =>
      preset.needsBaseUrl && !cleanUrl ? '' : cleanUrl + endpoint
    props.form.setValue(
      'authorization_endpoint',
      resolveEndpoint(preset.authorization_endpoint),
      { shouldDirty: true }
    )
    props.form.setValue(
      'token_endpoint',
      resolveEndpoint(preset.token_endpoint),
      {
        shouldDirty: true,
      }
    )
    props.form.setValue(
      'user_info_endpoint',
      resolveEndpoint(preset.user_info_endpoint),
      { shouldDirty: true }
    )
  }

  return (
    <div className='space-y-3 rounded-lg border border-dashed p-4'>
      <p className='text-sm font-medium'>{t('Quick Setup from Preset')}</p>
      <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
        <div className='space-y-1.5'>
          <Label htmlFor={presetId}>{t('Preset Template')}</Label>
          <Select
            items={[
              ...OAUTH_PRESETS.map((preset) => ({
                value: preset.key,
                label: preset.name,
              })),
            ]}
            value={selectedPreset}
            onValueChange={(v) => v !== null && handlePresetChange(v)}
          >
            <SelectTrigger id={presetId} className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {OAUTH_PRESETS.map((preset) => (
                  <SelectItem key={preset.key} value={preset.key}>
                    {preset.name}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
        {currentPreset?.needsBaseUrl !== false && (
          <div className='space-y-1.5'>
            <Label htmlFor={baseUrlId}>{t('Base URL')}</Label>
            <Input
              id={baseUrlId}
              placeholder={t('https://your-server.example.com')}
              value={baseUrl}
              onChange={(e) => handleBaseUrlChange(e.target.value)}
            />
          </div>
        )}
      </div>
    </div>
  )
}
