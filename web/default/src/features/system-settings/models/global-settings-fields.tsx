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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import type {
  GlobalJsonFieldName,
  GlobalModelSettingsForm,
} from './global-settings-form'
import { ProtocolBridgeSettings } from './protocol-bridge-settings'

const thinkingBlacklistExample = JSON.stringify(
  ['moonshotai/kimi-k2-thinking', 'kimi-k2-thinking'],
  null,
  2
)

type SettingsFieldsProps = { form: GlobalModelSettingsForm }

function formatJsonField(
  form: GlobalModelSettingsForm,
  field: GlobalJsonFieldName,
  t: TFunction
) {
  const raw = form.getValues(field)
  if (!raw || !raw.trim()) return
  try {
    form.setValue(field, JSON.stringify(JSON.parse(raw), null, 2), {
      shouldDirty: true,
    })
  } catch {
    toast.error(t('Invalid JSON format'))
  }
}

function PassThroughField({ form }: SettingsFieldsProps) {
  const { t } = useTranslation()
  return (
    <FormField
      control={form.control}
      name='global.pass_through_request_enabled'
      render={({ field }) => (
        <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
          <div className='space-y-0.5'>
            <FormLabel className='text-base'>
              {t('Enable Request Passthrough')}
            </FormLabel>
            <FormDescription>
              {t(
                'Forward requests directly to upstream providers without any post-processing.'
              )}
            </FormDescription>
          </div>
          <FormControl>
            <Switch checked={field.value} onCheckedChange={field.onChange} />
          </FormControl>
        </FormItem>
      )}
    />
  )
}

function ThinkingBlacklistField({ form }: SettingsFieldsProps) {
  const { t } = useTranslation()
  return (
    <FormField
      control={form.control}
      name='global.thinking_model_blacklist'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Disable thinking processing models')}</FormLabel>
          <FormControl>
            <Textarea
              rows={4}
              placeholder={`${t('Example:')}\n${thinkingBlacklistExample}`}
              {...field}
            />
          </FormControl>
          <FormDescription>
            {t(
              'Models listed here will not automatically append or remove -thinking / -nothinking suffixes.'
            )}
          </FormDescription>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() =>
              formatJsonField(form, 'global.thinking_model_blacklist', t)
            }
          >
            {t('Format JSON')}
          </Button>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function KeepAliveToggle({ form }: SettingsFieldsProps) {
  const { t } = useTranslation()
  return (
    <FormField
      control={form.control}
      name='general_setting.ping_interval_enabled'
      render={({ field }) => (
        <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
          <div className='space-y-0.5'>
            <FormLabel className='text-base'>{t('Keep-alive Ping')}</FormLabel>
            <FormDescription>
              {t(
                'Periodically send ping frames to keep streaming connections active.'
              )}
            </FormDescription>
          </div>
          <FormControl>
            <Switch checked={field.value} onCheckedChange={field.onChange} />
          </FormControl>
        </FormItem>
      )}
    />
  )
}

function PingIntervalField({ form }: SettingsFieldsProps) {
  const { t } = useTranslation()
  const pingEnabled = form.watch('general_setting.ping_interval_enabled')
  return (
    <FormField
      control={form.control}
      name='general_setting.ping_interval_seconds'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Ping Interval (seconds)')}</FormLabel>
          <FormControl>
            <Input
              type='number'
              min={1}
              disabled={!pingEnabled}
              className='w-24'
              value={field.value == null ? '' : String(field.value)}
              onChange={(event) => field.onChange(event.target.value)}
              onBlur={field.onBlur}
              name={field.name}
              ref={field.ref}
            />
          </FormControl>
          <FormDescription>
            {t('Recommended to keep this high to avoid upstream throttling.')}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

export function GlobalSettingsFields({ form }: SettingsFieldsProps) {
  return (
    <>
      <PassThroughField form={form} />
      <ThinkingBlacklistField form={form} />
      <Separator />
      <ProtocolBridgeSettings form={form} />
      <Separator />
      <KeepAliveToggle form={form} />
      <PingIntervalField form={form} />
    </>
  )
}
