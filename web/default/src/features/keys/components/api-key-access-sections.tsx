import { useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { ChevronDown, Settings2, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { MultiSelect } from '@/components/multi-select'
import type { ApiKeyFormValues } from '../lib'
import { ApiKeyFormSection } from './api-key-form-section'

type ApiKeyForm = UseFormReturn<ApiKeyFormValues>

export function ApiKeyAccessSections(props: {
  form: ApiKeyForm
  models: string[]
  quotaLabel: string
  quotaPlaceholder: string
  currencyLabel: string
  tokensOnly: boolean
}) {
  return (
    <>
      <QuotaSection {...props} />
      <AdvancedSection form={props.form} models={props.models} />
    </>
  )
}

function QuotaSection(props: {
  form: ApiKeyForm
  quotaLabel: string
  quotaPlaceholder: string
  currencyLabel: string
  tokensOnly: boolean
}) {
  const { t } = useTranslation()
  const unlimited = props.form.watch('unlimited_quota')
  return (
    <ApiKeyFormSection
      title={t('Quota Settings')}
      description={t('Set quota amount and limits')}
      icon={WalletCards}
    >
      {!unlimited && <QuotaAmountField {...props} />}
      <UnlimitedQuotaField form={props.form} />
    </ApiKeyFormSection>
  )
}

function QuotaAmountField(props: {
  form: ApiKeyForm
  quotaLabel: string
  quotaPlaceholder: string
  currencyLabel: string
  tokensOnly: boolean
}) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='remain_quota_dollars'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{props.quotaLabel}</FormLabel>
          <FormControl>
            <Input
              {...field}
              type='number'
              step={props.tokensOnly ? 1 : 0.01}
              placeholder={props.quotaPlaceholder}
              onChange={(event) =>
                field.onChange(parseFloat(event.target.value) || 0)
              }
            />
          </FormControl>
          <FormDescription>
            {props.tokensOnly
              ? t('Enter the quota amount in tokens')
              : t('Enter the quota amount in {{currency}}', {
                  currency: props.currencyLabel,
                })}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function UnlimitedQuotaField(props: { form: ApiKeyForm }) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='unlimited_quota'
      render={({ field }) => (
        <FormItem className='flex min-h-16 flex-row items-center justify-between gap-3 rounded-lg border px-3 py-2.5 sm:min-h-20 sm:gap-4 sm:px-4 sm:py-3'>
          <div className='space-y-0.5'>
            <FormLabel className='text-sm'>{t('Unlimited Quota')}</FormLabel>
            <FormDescription className='text-xs'>
              {t('Enable unlimited quota for this API key')}
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

function AdvancedSection(props: { form: ApiKeyForm; models: string[] }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <section className='bg-card rounded-lg border'>
        <CollapsibleTrigger
          render={
            <button
              type='button'
              className='hover:bg-muted/50 flex w-full items-center gap-2.5 px-3 py-2.5 text-left transition-colors sm:gap-3 sm:px-4 sm:py-3'
            />
          }
        >
          <div className='bg-muted text-muted-foreground flex size-8 shrink-0 items-center justify-center rounded-lg border sm:size-10'>
            <Settings2 className='size-4 sm:size-5' />
          </div>
          <div className='min-w-0 flex-1'>
            <h3 className='text-sm leading-none font-medium'>
              {t('Advanced Settings')}
            </h3>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Set API key access restrictions')}
            </p>
          </div>
          <ChevronDown
            className={cn(
              'text-muted-foreground size-4 shrink-0 transition-transform',
              open && 'rotate-180'
            )}
          />
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className='space-y-3 border-t p-3 sm:space-y-4 sm:p-4'>
            <ModelLimitsField form={props.form} models={props.models} />
            <IpWhitelistField form={props.form} />
          </div>
        </CollapsibleContent>
      </section>
    </Collapsible>
  )
}

function ModelLimitsField(props: { form: ApiKeyForm; models: string[] }) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='model_limits'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Model Limits')}</FormLabel>
          <FormControl>
            <MultiSelect
              options={props.models.map((model) => ({
                label: model,
                value: model,
              }))}
              selected={field.value}
              onChange={field.onChange}
              placeholder={t('Select models (empty for allow all)')}
            />
          </FormControl>
          <FormDescription>
            {t('Limit which models can be used with this key')}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function IpWhitelistField(props: { form: ApiKeyForm }) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='allow_ips'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('IP Whitelist (supports CIDR)')}</FormLabel>
          <FormControl>
            <Textarea
              {...field}
              className='min-h-20 resize-none'
              placeholder={t('One IP per line (empty for no restriction)')}
              rows={3}
            />
          </FormControl>
          <FormDescription>
            {t(
              'Do not over-trust this feature. IP may be spoofed. Please use with nginx, CDN and other gateways.'
            )}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
