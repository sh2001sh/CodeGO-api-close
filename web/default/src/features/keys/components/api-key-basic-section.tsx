import type { UseFormReturn } from 'react-hook-form'
import { KeyRound } from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
import { DateTimePicker } from '@/components/datetime-picker'
import type { ApiKeyFormValues } from '../lib'
import { ApiKeyAvailableModelsDialog } from './api-key-available-models-dialog'
import { ApiKeyFormSection } from './api-key-form-section'
import {
  ApiKeyGroupCombobox,
  type ApiKeyGroupOption,
} from './api-key-group-combobox'

type ApiKeyForm = UseFormReturn<ApiKeyFormValues>

export function ApiKeyBasicSection(props: {
  form: ApiKeyForm
  groups: ApiKeyGroupOption[]
  isUpdate: boolean
}) {
  const { t } = useTranslation()
  const selectedGroup = props.form.watch('group')
  return (
    <ApiKeyFormSection
      title={t('Basic Information')}
      description={t('Set API key basic information')}
      icon={KeyRound}
    >
      <NameField form={props.form} />
      <GroupField form={props.form} groups={props.groups} />
      {(selectedGroup?.startsWith('market:') || selectedGroup === 'auto') && (
        <MarketplaceMultiplierLimitField form={props.form} />
      )}
      <ExpirationField form={props.form} />
      {!props.isUpdate && <QuantityField form={props.form} />}
    </ApiKeyFormSection>
  )
}

function MarketplaceMultiplierLimitField(props: { form: ApiKeyForm }) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='marketplace_multiplier_limit'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('路由倍率上限')}</FormLabel>
          <FormControl>
            <div className='relative max-w-52'>
              <Input
                type='number'
                inputMode='decimal'
                min='0'
                max='1000000'
                step='any'
                value={field.value ?? ''}
                onBlur={field.onBlur}
                name={field.name}
                ref={field.ref}
                onChange={(event) => {
                  const value = event.target.value
                  field.onChange(value === '' ? undefined : Number(value))
                }}
                className='pr-8 tabular-nums'
              />
              <span className='text-muted-foreground pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-sm'>
                x
              </span>
            </div>
          </FormControl>
          <FormDescription>
            {t(
              '第三方分组或 Auto 路由池中的渠道倍率超过该值时跳过；设置为 0 表示不限制。'
            )}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function NameField(props: { form: ApiKeyForm }) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='name'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Name')}</FormLabel>
          <FormControl>
            <Input {...field} placeholder={t('Enter a name')} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function GroupField(props: { form: ApiKeyForm; groups: ApiKeyGroupOption[] }) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='group'
      render={({ field }) => {
        const selectedOption = props.groups.find(
          (option) => option.value === field.value
        )
        return (
          <FormItem>
            <FormLabel>{t('Group')}</FormLabel>
            <FormControl>
              <ApiKeyGroupCombobox
                options={props.groups}
                value={field.value}
                onValueChange={field.onChange}
                placeholder={t('Select a group')}
              />
            </FormControl>
            {field.value?.startsWith('market:') && (
              <FormDescription>
                {t('第三方分组可按所示倍率使用套餐或通用余额。')}
              </FormDescription>
            )}
            <div className='mt-2 flex flex-wrap gap-2'>
              <ApiKeyAvailableModelsDialog option={selectedOption} />
            </div>
            <FormMessage />
          </FormItem>
        )
      }}
    />
  )
}

function ExpirationField(props: { form: ApiKeyForm }) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='expired_time'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Expiration Time')}</FormLabel>
          <div className='grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center'>
            <FormControl>
              <DateTimePicker
                value={field.value}
                onChange={field.onChange}
                placeholder={t('Never expires')}
                className='min-w-0 [&_input[type=time]]:w-24 sm:[&_input[type=time]]:w-32'
              />
            </FormControl>
            <ExpirationPresets onChange={field.onChange} />
          </div>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}

function ExpirationPresets(props: {
  onChange: (value: Date | undefined) => void
}) {
  const { t } = useTranslation()
  const setExpiry = (months: number, days: number, hours: number) => {
    if (months === 0 && days === 0 && hours === 0) {
      props.onChange(undefined)
      return
    }
    const next = new Date()
    next.setMonth(next.getMonth() + months)
    next.setDate(next.getDate() + days)
    next.setHours(next.getHours() + hours)
    props.onChange(next)
  }
  const presets = [
    [t('Never'), 0, 0, 0],
    [t('1 Month'), 1, 0, 0],
    [t('1 Day'), 0, 1, 0],
    [t('1 Hour'), 0, 0, 1],
  ] as const
  return (
    <div className='grid grid-cols-4 gap-2 sm:flex'>
      {presets.map(([label, months, days, hours]) => (
        <Button
          key={label}
          type='button'
          variant='outline'
          size='sm'
          className='px-2 text-xs sm:px-3 sm:text-sm'
          onClick={() => setExpiry(months, days, hours)}
        >
          {label}
        </Button>
      ))}
    </div>
  )
}

function QuantityField(props: { form: ApiKeyForm }) {
  const { t } = useTranslation()
  return (
    <FormField
      control={props.form.control}
      name='tokenCount'
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t('Quantity')}</FormLabel>
          <FormControl>
            <Input
              {...field}
              type='number'
              min='1'
              placeholder={t('Number of keys to create')}
              onChange={(event) =>
                field.onChange(parseInt(event.target.value, 10) || 1)
              }
            />
          </FormControl>
          <FormDescription>
            {t(
              'Create multiple API keys at once (random suffix will be added to names)'
            )}
          </FormDescription>
          <FormMessage />
        </FormItem>
      )}
    />
  )
}
