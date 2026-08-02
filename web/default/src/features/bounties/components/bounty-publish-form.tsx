import { Controller, type UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { DrawerFooter } from '@/components/ui/drawer'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { DateTimePicker } from '@/components/datetime-picker'
import type { BountyFormValues } from '../lib/bounty-form'

interface BountyPublishFormProps {
  form: UseFormReturn<BountyFormValues>
  onCancel: () => void
  onReview: () => void
}

export function BountyPublishForm(props: BountyPublishFormProps) {
  const { t } = useTranslation()
  const form = props.form
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault()
        props.onReview()
      }}
      className='flex min-h-0 flex-1 flex-col'
    >
      <div className='min-h-0 flex-1 space-y-5 overflow-y-auto p-5'>
        <div className='space-y-2'>
          <Label htmlFor='bounty-title'>{t('Task title')} *</Label>
          <Input
            id='bounty-title'
            {...form.register('title')}
            placeholder={t('Example: improve the dashboard loading experience')}
            aria-invalid={Boolean(form.formState.errors.title)}
          />
          {form.formState.errors.title ? (
            <p className='text-destructive text-xs'>
              {t(form.formState.errors.title.message ?? '')}
            </p>
          ) : null}
        </div>
        <div className='space-y-2'>
          <Label htmlFor='bounty-description'>{t('Task description')} *</Label>
          <Textarea
            id='bounty-description'
            {...form.register('description')}
            placeholder={t(
              'Describe the background, goal, and expected result. The executor can ask for missing material later.'
            )}
            className='min-h-36 resize-y'
            aria-invalid={Boolean(form.formState.errors.description)}
          />
          {form.formState.errors.description ? (
            <p className='text-destructive text-xs'>
              {t(form.formState.errors.description.message ?? '')}
            </p>
          ) : null}
          <p className='text-muted-foreground text-xs'>
            {t(
              'Executors can request requirements or materials during development. Reply in the task discussion so the decision is auditable.'
            )}
          </p>
        </div>
        <div className='space-y-2'>
          <Label htmlFor='bounty-repo'>
            {t('GitHub repository or project URL')} *
          </Label>
          <Input
            id='bounty-repo'
            {...form.register('repo_url')}
            placeholder='https://github.com/owner/repository'
            aria-invalid={Boolean(form.formState.errors.repo_url)}
          />
          {form.formState.errors.repo_url ? (
            <p className='text-destructive text-xs'>
              {t(form.formState.errors.repo_url.message ?? '')}
            </p>
          ) : null}
        </div>
        <div className='grid gap-4 sm:grid-cols-2'>
          <div className='space-y-2'>
            <Label htmlFor='bounty-type'>{t('Task type')}</Label>
            <NativeSelect
              id='bounty-type'
              value={form.watch('task_type')}
              onChange={(event) =>
                form.setValue(
                  'task_type',
                  event.target.value as BountyFormValues['task_type']
                )
              }
              className='w-full'
            >
              <NativeSelectOption value='general'>
                {t('General coding')}
              </NativeSelectOption>
              <NativeSelectOption value='ui'>
                {t('UI / interaction')}
              </NativeSelectOption>
              <NativeSelectOption value='frontend'>
                {t('Frontend')}
              </NativeSelectOption>
              <NativeSelectOption value='backend'>
                {t('Backend')}
              </NativeSelectOption>
            </NativeSelect>
          </div>
          <div className='space-y-2'>
            <Label htmlFor='bounty-tags'>{t('Technology tags')}</Label>
            <Input
              id='bounty-tags'
              {...form.register('tags')}
              placeholder={t('React, Go, performance')}
            />
            <p className='text-muted-foreground text-xs'>
              {t('Separate tags with commas; up to 12 tags.')}
            </p>
          </div>
        </div>
        <div className='grid gap-4 sm:grid-cols-2'>
          <div className='space-y-2'>
            <Label htmlFor='bounty-wallet-type'>
              {t('Reward quota type')} *
            </Label>
            <NativeSelect
              id='bounty-wallet-type'
              value={form.watch('reward_wallet_type')}
              onChange={(event) =>
                form.setValue(
                  'reward_wallet_type',
                  event.target.value as BountyFormValues['reward_wallet_type']
                )
              }
              className='w-full'
            >
              <NativeSelectOption value='wallet'>
                {t('Normal quota')}
              </NativeSelectOption>
              <NativeSelectOption value='claude_wallet'>
                {t('Claude quota')}
              </NativeSelectOption>
            </NativeSelect>
          </div>
          <div className='space-y-2'>
            <Label htmlFor='bounty-amount'>{t('Reward amount (USD)')} *</Label>
            <Input
              id='bounty-amount'
              type='number'
              min={0.01}
              step={0.01}
              {...form.register('reward_amount', { valueAsNumber: true })}
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Enter the reward in USD. It is converted to platform quota units when submitted.'
              )}
            </p>
          </div>
        </div>
        <div className='space-y-2'>
          <Label htmlFor='bounty-deadline'>{t('Delivery deadline')} *</Label>
          <Controller
            control={form.control}
            name='deadline_at'
            render={({ field }) => (
              <DateTimePicker
                id='bounty-deadline'
                value={field.value ? new Date(field.value) : undefined}
                onChange={(date) => field.onChange(formatLocalDateTime(date))}
                placeholder={t('Select delivery deadline')}
                ariaInvalid={Boolean(form.formState.errors.deadline_at)}
                minDateTime={getNextMinute()}
              />
            )}
          />
          {form.formState.errors.deadline_at ? (
            <p className='text-destructive text-xs'>
              {t(form.formState.errors.deadline_at.message ?? '')}
            </p>
          ) : null}
        </div>
      </div>
      <DrawerFooter className='border-border/70 border-t'>
        <Button type='button' variant='outline' onClick={props.onCancel}>
          {t('Cancel')}
        </Button>
        <Button type='submit'>{t('Review and publish')}</Button>
      </DrawerFooter>
    </form>
  )
}

function formatLocalDateTime(date: Date | undefined) {
  if (!date) return ''
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function getNextMinute() {
  return new Date(Math.ceil(Date.now() / 60000) * 60000)
}
