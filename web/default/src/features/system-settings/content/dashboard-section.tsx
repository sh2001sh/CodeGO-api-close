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
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { handleSettingsAction } from '../utils/settings-action'

const dataDashboardSchema = z.object({
  DataExportEnabled: z.boolean(),
  DataExportInterval: z.number().int().min(1).max(1440),
  DataExportDefaultTime: z.enum(['hour', 'day', 'week']),
})

type DataDashboardFormValues = z.infer<typeof dataDashboardSchema>

type DashboardSectionProps = {
  defaultValues: DataDashboardFormValues
}

const granularityOptions = [
  { label: 'Hour', value: 'hour' },
  { label: 'Day', value: 'day' },
  { label: 'Week', value: 'week' },
]

export function DashboardSection({ defaultValues }: DashboardSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<DataDashboardFormValues>({
    resolver: zodResolver(dataDashboardSchema),
    defaultValues,
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (values: DataDashboardFormValues) => {
    const updates = Object.entries(values).filter(
      ([key, value]) =>
        value !== defaultValues[key as keyof DataDashboardFormValues]
    )

    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value })
    }
    form.reset(values)
  }

  const isEnabled = form.watch('DataExportEnabled')

  return (
    <SettingsSection
      title={t('Data Dashboard')}
      description={t('Configure data export settings for dashboard')}
    >
      <Form {...form}>
        <form
          onSubmit={handleSettingsAction(form.handleSubmit(onSubmit))}
          className='space-y-6'
        >
          <FormField
            control={form.control}
            name='DataExportEnabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>
                    {t('Enable Data Dashboard')}
                  </FormLabel>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          <div className='grid gap-6 sm:grid-cols-2'>
            <FormField
              control={form.control}
              name='DataExportInterval'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Refresh interval (minutes)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      max={1440}
                      step={1}
                      disabled={!isEnabled}
                      value={field.value}
                      onChange={(e) => field.onChange(e.target.valueAsNumber)}
                    />
                  </FormControl>
                  <FormDescription className='settings-field-fact'>
                    {t('> 1 min')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='DataExportDefaultTime'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Default time granularity')}</FormLabel>
                  <Select
                    items={[
                      ...granularityOptions.map((option) => ({
                        value: option.value,
                        label: t(option.label),
                      })),
                    ]}
                    onValueChange={field.onChange}
                    value={field.value}
                    disabled={!isEnabled}
                  >
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent alignItemWithTrigger={false}>
                      <SelectGroup>
                        {granularityOptions.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {t(option.label)}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FormDescription className='settings-field-fact'>
                    {t('Hourly aggregation')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='settings-save-bar'>
            <Button type='submit' disabled={updateOption.isPending}>
              {updateOption.isPending ? t('Saving...') : t('Save Changes')}
            </Button>
          </div>
        </form>
      </Form>
    </SettingsSection>
  )
}
