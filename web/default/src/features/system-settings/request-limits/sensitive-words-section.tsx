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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { handleSettingsAction } from '../utils/settings-action'
import {
  RECOMMENDED_GUARD_REVIEW_RULES,
  RECOMMENDED_HARD_BLOCK_RULES,
  presetRulesToTextareaValue,
} from './security-rule-presets'

const sensitiveSchema = z.object({
  CheckSensitiveEnabled: z.boolean(),
  CheckSensitiveOnPromptEnabled: z.boolean(),
  StopOnSensitiveEnabled: z.boolean(),
  SensitiveWords: z.string().optional(),
  PromptAuditReviewRules: z.string().optional(),
})

type SensitiveFormValues = z.infer<typeof sensitiveSchema>

type SensitiveWordsSectionProps = {
  defaultValues: SensitiveFormValues
}

export function SensitiveWordsSection({
  defaultValues,
}: SensitiveWordsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const form = useForm<SensitiveFormValues>({
    resolver: zodResolver(sensitiveSchema),
    defaultValues,
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (values: SensitiveFormValues) => {
    const updates = Object.entries(values).filter(
      ([key, value]) =>
        value !== defaultValues[key as keyof SensitiveFormValues]
    )

    for (const [key, value] of updates) {
      await updateOption.mutateAsync({ key, value: value ?? '' })
    }
    form.reset(values)
  }

  return (
    <SettingsSection
      title={t('Sensitive Words')}
      description={t('Configure sensitive word filtering')}
    >
      <Form {...form}>
        <form
          onSubmit={handleSettingsAction(form.handleSubmit(onSubmit))}
          className='space-y-6'
        >
          <div className='bg-muted/30 rounded-lg border p-4'>
            <p className='text-sm font-medium'>{t('Recommended layering')}</p>
            <p className='text-muted-foreground mt-1 text-base'>
              {t('Hard block / Guard review')}
            </p>
          </div>

          <div className='space-y-4'>
            <FormField
              control={form.control}
              name='CheckSensitiveEnabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Enable filtering')}
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

            <FormField
              control={form.control}
              name='CheckSensitiveOnPromptEnabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Inspect user prompts')}
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

            <FormField
              control={form.control}
              name='StopOnSensitiveEnabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Block on hard rule match')}
                    </FormLabel>
                    <FormDescription className='settings-field-fact'>
                      {t('Disabled: log + forward')}
                    </FormDescription>
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
          </div>

          <FormField
            control={form.control}
            name='SensitiveWords'
            render={({ field }) => (
              <FormItem>
                <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                  <div className='space-y-1'>
                    <FormLabel>{t('Hard-block rules')}</FormLabel>
                    <FormDescription className='settings-field-fact'>
                      {t('One rule per line')}
                    </FormDescription>
                  </div>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      form.setValue(
                        'SensitiveWords',
                        presetRulesToTextareaValue(
                          RECOMMENDED_HARD_BLOCK_RULES
                        ),
                        { shouldDirty: true }
                      )
                    }
                  >
                    {t('Apply recommended hard-block rules')}
                  </Button>
                </div>
                <FormControl>
                  <Textarea
                    rows={12}
                    placeholder={t(
                      'contains:sql injection payload\ncontains:bypass rate limit'
                    )}
                    className='font-mono text-sm'
                    {...field}
                  />
                </FormControl>
                <FormDescription className='settings-field-fact'>
                  {t('keyword / contains:phrase / re:regex')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='PromptAuditReviewRules'
            render={({ field }) => (
              <FormItem>
                <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
                  <div className='space-y-1'>
                    <FormLabel>{t('Guard review rules')}</FormLabel>
                    <FormDescription className='settings-field-fact'>
                      {t('Match → Guard · Other → upstream')}
                    </FormDescription>
                  </div>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      form.setValue(
                        'PromptAuditReviewRules',
                        presetRulesToTextareaValue(
                          RECOMMENDED_GUARD_REVIEW_RULES
                        ),
                        { shouldDirty: true }
                      )
                    }
                  >
                    {t('Apply recommended review rules')}
                  </Button>
                </div>
                <FormControl>
                  <Textarea
                    rows={12}
                    placeholder={t(
                      'contains:jailbreak prompt\nre:(?i)\\b(ctf|pentest)\\b'
                    )}
                    className='font-mono text-sm'
                    {...field}
                  />
                </FormControl>

                <FormMessage />
              </FormItem>
            )}
          />

          <div className='settings-save-bar'>
            <Button type='submit' disabled={updateOption.isPending}>
              {updateOption.isPending
                ? t('Saving...')
                : t('Save security rules')}
            </Button>
          </div>
        </form>
      </Form>
    </SettingsSection>
  )
}
