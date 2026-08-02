import { useMemo } from 'react'
import { z } from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CalendarClock, Percent } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
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
import { Switch } from '@/components/ui/switch'
import { updateSystemOption } from '../api'
import { SettingsSection } from '../components/settings-section'

type CampaignDefaults = {
  enabled: boolean
  multiplier: number
  startAt: number
  endAt: number
}

const campaignSchema = z
  .object({
    enabled: z.boolean(),
    discount: z.number().min(0.1).max(9.9),
    startAt: z.string(),
    endAt: z.string(),
  })
  .superRefine((values, context) => {
    if (!values.enabled) return
    const start = new Date(values.startAt).getTime()
    const end = new Date(values.endAt).getTime()
    if (!values.startAt || Number.isNaN(start)) {
      context.addIssue({
        code: 'custom',
        path: ['startAt'],
        message: 'Select a valid start time',
      })
    }
    if (!values.endAt || Number.isNaN(end) || end <= start) {
      context.addIssue({
        code: 'custom',
        path: ['endAt'],
        message: 'End time must be after start time',
      })
    }
  })

type CampaignValues = z.infer<typeof campaignSchema>

function toLocalDateTime(timestamp: number): string {
  if (!timestamp) return ''
  const date = new Date(timestamp * 1000)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

function toTimestamp(value: string): number {
  return value ? Math.floor(new Date(value).getTime() / 1000) : 0
}

export function FirstPurchaseDiscountSection(props: {
  defaultValues: CampaignDefaults
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const defaults = useMemo<CampaignValues>(
    () => ({
      enabled: props.defaultValues.enabled,
      discount: (props.defaultValues.multiplier || 0.8) * 10,
      startAt: toLocalDateTime(props.defaultValues.startAt),
      endAt: toLocalDateTime(props.defaultValues.endAt),
    }),
    [props.defaultValues]
  )
  const form = useForm<CampaignValues>({
    resolver: zodResolver(campaignSchema),
    defaultValues: defaults,
  })
  const enabled = form.watch('enabled')
  const startAt = form.watch('startAt')
  const endAt = form.watch('endAt')
  const now = Date.now()
  const active =
    enabled &&
    new Date(startAt).getTime() <= now &&
    new Date(endAt).getTime() >= now

  const saveMutation = useMutation({
    mutationFn: async (values: CampaignValues) => {
      const updates = [
        {
          key: 'payment_setting.first_purchase_discount_multiplier',
          value: String(values.discount / 10),
        },
        {
          key: 'payment_setting.first_purchase_discount_start_at',
          value: String(toTimestamp(values.startAt)),
        },
        {
          key: 'payment_setting.first_purchase_discount_end_at',
          value: String(toTimestamp(values.endAt)),
        },
      ]
      if (!values.enabled) {
        updates.unshift({
          key: 'payment_setting.first_purchase_discount_enabled',
          value: 'false',
        })
      } else {
        updates.push({
          key: 'payment_setting.first_purchase_discount_enabled',
          value: 'true',
        })
      }
      for (const update of updates) {
        const response = await updateSystemOption(update)
        if (!response.success)
          throw new Error(response.message || t('Failed to update setting'))
      }
      return values
    },
    onSuccess: (values) => {
      form.reset(values)
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      toast.success(t('First purchase campaign updated'))
    },
    onError: (error: Error) => toast.error(error.message),
  })

  return (
    <SettingsSection
      title={t('First purchase campaign')}
      description={t(
        'Offer a time-limited discount on each user’s first successful monthly plan purchase. Starter, daily, and weekly plans are excluded.'
      )}
    >
      <Form {...form}>
        <form
          onSubmit={form.handleSubmit((values) => saveMutation.mutate(values))}
          className='space-y-5'
        >
          <div className='flex flex-col gap-4 rounded-lg border p-4 sm:flex-row sm:items-center sm:justify-between'>
            <div className='flex items-start gap-3'>
              <div className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
                <Percent className='size-4' aria-hidden='true' />
              </div>
              <div className='space-y-1'>
                <div className='flex flex-wrap items-center gap-2'>
                  <p className='text-sm font-semibold'>
                    {t('Campaign status')}
                  </p>
                  <Badge variant={active ? 'default' : 'outline'}>
                    {active
                      ? t('Active')
                      : enabled
                        ? t('Scheduled or ended')
                        : t('Disabled')}
                  </Badge>
                </div>
                <p className='text-muted-foreground max-w-2xl text-sm'>
                  {t(
                    'Plan-purchase eligibility is reserved when an order is created and consumed only after a successful payment.'
                  )}
                </p>
              </div>
            </div>
            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <FormItem className='flex items-center gap-3 space-y-0'>
                  <FormLabel>{t('Enable campaign')}</FormLabel>
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

          <div className='grid gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='discount'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Discount (折)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      step='0.1'
                      min='0.1'
                      max='9.9'
                      value={field.value}
                      onBlur={field.onBlur}
                      onChange={(event) =>
                        field.onChange(event.target.valueAsNumber)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('For example, enter 8 for an 80% checkout price.')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='startAt'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Starts at')}</FormLabel>
                  <FormControl>
                    <Input
                      type='datetime-local'
                      disabled={!enabled}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='endAt'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Ends at')}</FormLabel>
                  <FormControl>
                    <Input
                      type='datetime-local'
                      disabled={!enabled}
                      {...field}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='flex items-center justify-between gap-4 border-t pt-4'>
            <p className='text-muted-foreground flex items-center gap-2 text-xs'>
              <CalendarClock className='size-4' aria-hidden='true' />
              {t(
                'Times use the administrator’s local timezone and are stored as absolute timestamps.'
              )}
            </p>
            <Button
              type='submit'
              disabled={!form.formState.isDirty || saveMutation.isPending}
            >
              {saveMutation.isPending ? t('Saving...') : t('Save campaign')}
            </Button>
          </div>
        </form>
      </Form>
    </SettingsSection>
  )
}
