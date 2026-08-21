import { useState } from 'react'
import { CircleDollarSign, Save, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Switch } from '@/components/ui/switch'
import {
  channelPriceDraftToValue,
  channelPriceToDraft,
  validateChannelPriceDraft,
  type ChannelBillingMode,
  type ChannelPriceDraft,
} from '../lib/channel-model-price'
import type { ChannelModelPrice } from '../types'
import { FormField } from './channel-form-layout'

export function ChannelPriceEditor(props: {
  price?: ChannelModelPrice
  onSave: (price: ChannelModelPrice) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState<ChannelPriceDraft>(() =>
    channelPriceToDraft(props.price)
  )

  const isEditing = Boolean(props.price) || editing

  if (!isEditing) {
    return (
      <Button
        type='button'
        variant='outline'
        size='sm'
        className='justify-self-start'
        onClick={() => setEditing(true)}
      >
        <CircleDollarSign />
        {t('手动配置价格')}
      </Button>
    )
  }

  const error = validateChannelPriceDraft(draft, t)
  return (
    <div className='space-y-3'>
      <BillingModePicker
        mode={draft.mode}
        onChange={(mode) => setDraft((current) => ({ ...current, mode }))}
      />
      {draft.mode === 'per_call' ? (
        <PriceInput
          label={t('每次请求价格')}
          value={draft.pricePerCall}
          onChange={(value) =>
            setDraft((current) => ({ ...current, pricePerCall: value }))
          }
        />
      ) : (
        <TokenPriceFields draft={draft} onChange={setDraft} />
      )}
      {error && (
        <p className='text-destructive text-xs' role='alert'>
          {error}
        </p>
      )}
      <div className='flex flex-wrap gap-2'>
        <Button
          type='button'
          size='sm'
          disabled={Boolean(error)}
          onClick={() => props.onSave(channelPriceDraftToValue(draft))}
        >
          <Save />
          {t('保存价格')}
        </Button>
        <Button
          type='button'
          size='sm'
          variant='ghost'
          onClick={() => {
            if (props.price) props.onRemove()
            else setEditing(false)
          }}
        >
          <X />
          {props.price ? t('移除') : t('取消')}
        </Button>
      </div>
    </div>
  )
}

function BillingModePicker(props: {
  mode: ChannelBillingMode
  onChange: (mode: ChannelBillingMode) => void
}) {
  const { t } = useTranslation()
  return (
    <RadioGroup
      value={props.mode}
      onValueChange={(value) => props.onChange(value as ChannelBillingMode)}
      className='grid gap-2 sm:grid-cols-2'
      aria-label={t('计费方式')}
    >
      <label className='border-border flex cursor-pointer items-start gap-2 rounded-md border px-3 py-2'>
        <RadioGroupItem value='token' className='mt-0.5' />
        <span>
          <span className='block text-sm font-medium'>{t('按量计费')}</span>
          <span className='text-muted-foreground block text-xs'>
            {t('分别设置输入、输出及可选缓存价格')}
          </span>
        </span>
      </label>
      <label className='border-border flex cursor-pointer items-start gap-2 rounded-md border px-3 py-2'>
        <RadioGroupItem value='per_call' className='mt-0.5' />
        <span>
          <span className='block text-sm font-medium'>{t('按次计费')}</span>
          <span className='text-muted-foreground block text-xs'>
            {t('每次成功请求收取固定价格')}
          </span>
        </span>
      </label>
    </RadioGroup>
  )
}

function TokenPriceFields(props: {
  draft: ChannelPriceDraft
  onChange: (draft: ChannelPriceDraft) => void
}) {
  const { t } = useTranslation()
  const update = (patch: Partial<ChannelPriceDraft>) =>
    props.onChange({ ...props.draft, ...patch })
  return (
    <div className='space-y-3'>
      <div className='grid gap-2 sm:grid-cols-2'>
        <PriceInput
          label={t('输入 / 百万 Token')}
          value={props.draft.inputPrice}
          onChange={(value) => update({ inputPrice: value })}
        />
        <PriceInput
          label={t('输出 / 百万 Token')}
          value={props.draft.outputPrice}
          onChange={(value) => update({ outputPrice: value })}
        />
      </div>
      <OptionalCachePrice
        label={t('缓存读取价格')}
        description={t('启用后按每百万缓存读取 Token 单独计价')}
        enabled={props.draft.cacheReadEnabled}
        value={props.draft.cacheReadPrice}
        onEnabledChange={(enabled) => update({ cacheReadEnabled: enabled })}
        onValueChange={(value) => update({ cacheReadPrice: value })}
      />
      <OptionalCachePrice
        label={t('缓存写入价格')}
        description={t('启用后按每百万缓存写入 Token 单独计价')}
        enabled={props.draft.cacheWriteEnabled}
        value={props.draft.cacheWritePrice}
        onEnabledChange={(enabled) => update({ cacheWriteEnabled: enabled })}
        onValueChange={(value) => update({ cacheWritePrice: value })}
      />
    </div>
  )
}

function OptionalCachePrice(props: {
  label: string
  description: string
  enabled: boolean
  value: string
  onEnabledChange: (enabled: boolean) => void
  onValueChange: (value: string) => void
}) {
  return (
    <div className='border-border rounded-md border px-3 py-2.5'>
      <div className='flex items-start justify-between gap-3'>
        <div>
          <p className='text-sm font-medium'>{props.label}</p>
          <p className='text-muted-foreground mt-0.5 text-xs leading-5'>
            {props.description}
          </p>
        </div>
        <Switch
          checked={props.enabled}
          onCheckedChange={props.onEnabledChange}
          aria-label={props.label}
        />
      </div>
      {props.enabled && (
        <div className='mt-2 max-w-xs'>
          <PriceInput
            label={`${props.label} / 百万 Token`}
            value={props.value}
            allowZero
            onChange={props.onValueChange}
          />
        </div>
      )}
    </div>
  )
}

function PriceInput(props: {
  label: string
  value: string
  allowZero?: boolean
  onChange: (value: string) => void
}) {
  return (
    <FormField label={props.label}>
      <Input
        type='number'
        min={props.allowZero ? '0' : '0.000001'}
        max='1000000'
        step='any'
        inputMode='decimal'
        value={props.value}
        onChange={(event) => props.onChange(event.target.value)}
      />
    </FormField>
  )
}
