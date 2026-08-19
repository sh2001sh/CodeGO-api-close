import { useEffect, useMemo, useState } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { AlertTriangle, Check, CircleDollarSign, Save, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import type { ChannelFormInput } from '../lib/channel-form'
import { modelKey } from '../lib/channel-model-sync'
import type { ChannelModelPrice } from '../types'
import { FormField } from './channel-form-layout'

type ChannelForm = UseFormReturn<ChannelFormInput>

export function ChannelModelPrices(props: {
  form: ChannelForm
  selectedModels: string[]
}) {
  const { t } = useTranslation()
  const pricing = usePricingData()
  const prices = props.form.watch('model_prices')
  const sitePricedModels = useMemo(
    () => buildSitePricedModelSet(pricing.models),
    [pricing.models]
  )
  if (props.selectedModels.length === 0) return null

  const customModels = props.selectedModels.filter((model) => prices[model])
  const missingModels = pricing.isLoading
    ? []
    : props.selectedModels.filter(
        (model) => !sitePricedModels.has(modelKey(model)) && !prices[model]
      )
  const editableModels = props.selectedModels.filter(
    (model) => prices[model] || !sitePricedModels.has(modelKey(model))
  )
  const siteCoveredModels = props.selectedModels.filter(
    (model) => sitePricedModels.has(modelKey(model)) && !prices[model]
  )

  return (
    <div className='border-border overflow-hidden rounded-md border'>
      <PriceSectionHeader
        loading={pricing.isLoading}
        missingCount={missingModels.length}
        customCount={customModels.length}
      />
      {pricing.error && (
        <p className='border-border text-muted-foreground border-b px-3 py-2 text-xs'>
          {t('暂时无法读取站点价格状态，仍可为模型手动配置渠道价格。')}
        </p>
      )}
      {!pricing.isLoading && editableModels.length > 0 && (
        <div className='divide-border divide-y'>
          {editableModels.map((model) => (
            <ModelPriceRow
              key={model}
              model={model}
              price={prices[model]}
              sitePriced={sitePricedModels.has(modelKey(model))}
              onSave={(price) => setChannelPrice(props.form, model, price)}
              onRemove={() => removeChannelPrice(props.form, model)}
            />
          ))}
        </div>
      )}
      {siteCoveredModels.length > 0 && (
        <SiteCoveredModels models={siteCoveredModels} />
      )}
    </div>
  )
}

function PriceSectionHeader(props: {
  loading: boolean
  missingCount: number
  customCount: number
}) {
  const { t } = useTranslation()
  return (
    <div className='border-border flex flex-wrap items-start justify-between gap-2 border-b px-3 py-2.5'>
      <div>
        <p className='text-sm font-medium'>{t('模型价格覆盖')}</p>
        <p className='text-muted-foreground mt-1 text-xs leading-5'>
          {t('站点价格优先；缺价模型可在当前渠道单独补充输入、输出价格。')}
        </p>
      </div>
      <div className='flex gap-3 text-xs tabular-nums'>
        {props.loading ? (
          <span className='text-muted-foreground'>{t('正在核对价格…')}</span>
        ) : (
          <>
            <span
              className={
                props.missingCount
                  ? 'text-amber-700 dark:text-amber-400'
                  : 'text-muted-foreground'
              }
            >
              {t('待配置 {{count}}', { count: props.missingCount })}
            </span>
            <span className='text-muted-foreground'>
              {t('渠道价 {{count}}', { count: props.customCount })}
            </span>
          </>
        )}
      </div>
    </div>
  )
}

function ModelPriceRow(props: {
  model: string
  price?: ChannelModelPrice
  sitePriced: boolean
  onSave: (price: ChannelModelPrice) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className='grid gap-3 px-3 py-3 lg:grid-cols-[minmax(180px,1fr)_minmax(320px,1.4fr)] lg:items-start'>
      <div className='min-w-0'>
        <p className='truncate text-sm font-medium' title={props.model}>
          {props.model}
        </p>
        <p className='mt-1 flex items-center gap-1.5 text-xs'>
          {props.sitePriced ? (
            <>
              <Check className='size-3.5 text-emerald-600' />
              {t('站点价格已配置，渠道价格仅作备用')}
            </>
          ) : props.price ? (
            <>
              <CircleDollarSign className='text-primary size-3.5' />
              {t('使用当前渠道价格')}
            </>
          ) : (
            <>
              <AlertTriangle className='size-3.5 text-amber-600' />
              {t('站点尚未配置价格')}
            </>
          )}
        </p>
      </div>
      <ChannelPriceEditor
        price={props.price}
        onSave={props.onSave}
        onRemove={props.onRemove}
      />
    </div>
  )
}

function ChannelPriceEditor(props: {
  price?: ChannelModelPrice
  onSave: (price: ChannelModelPrice) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(Boolean(props.price))
  const [inputPrice, setInputPrice] = useState('')
  const [outputPrice, setOutputPrice] = useState('')
  useEffect(() => {
    setEditing(Boolean(props.price))
    setInputPrice(
      props.price ? String(props.price.input_price_per_million) : ''
    )
    setOutputPrice(
      props.price ? String(props.price.output_price_per_million) : ''
    )
  }, [props.price])

  if (!editing) {
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
  const error = validatePriceDraft(inputPrice, outputPrice, t)
  return (
    <div>
      <div className='grid gap-2 sm:grid-cols-2'>
        <PriceInput
          label={t('输入 / 百万 Token')}
          value={inputPrice}
          onChange={setInputPrice}
        />
        <PriceInput
          label={t('输出 / 百万 Token')}
          value={outputPrice}
          onChange={setOutputPrice}
        />
      </div>
      {error && (
        <p className='text-destructive mt-1.5 text-xs' role='alert'>
          {error}
        </p>
      )}
      <div className='mt-2 flex gap-2'>
        <Button
          type='button'
          size='sm'
          disabled={Boolean(error)}
          onClick={() => props.onSave(toModelPrice(inputPrice, outputPrice))}
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

function PriceInput(props: {
  label: string
  value: string
  onChange: (value: string) => void
}) {
  return (
    <FormField label={props.label}>
      <Input
        type='number'
        min='0.000001'
        max='1000000'
        step='any'
        inputMode='decimal'
        value={props.value}
        onChange={(event) => props.onChange(event.target.value)}
      />
    </FormField>
  )
}

function SiteCoveredModels(props: { models: string[] }) {
  const { t } = useTranslation()
  return (
    <details className='border-border border-t px-3 py-2.5'>
      <summary className='text-muted-foreground focus-visible:ring-ring cursor-pointer text-xs outline-none focus-visible:ring-2'>
        {t('{{count}} 个模型使用站点价格', { count: props.models.length })}
      </summary>
      <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs'>
        {props.models.map((model) => (
          <span key={model}>{model}</span>
        ))}
      </div>
    </details>
  )
}

function validatePriceDraft(
  input: string,
  output: string,
  t: (value: string) => string
): string {
  const values = [Number(input), Number(output)]
  if (!input || !output) return t('请输入输入和输出价格')
  if (values.some((value) => !Number.isFinite(value) || value <= 0))
    return t('价格必须大于 0')
  if (values.some((value) => value > 1_000_000))
    return t('价格不能超过 1000000')
  return ''
}

function toModelPrice(input: string, output: string): ChannelModelPrice {
  return {
    input_price_per_million: Number(input),
    output_price_per_million: Number(output),
  }
}

function setChannelPrice(
  form: ChannelForm,
  model: string,
  price: ChannelModelPrice
) {
  form.setValue(
    'model_prices',
    { ...form.getValues('model_prices'), [model]: price },
    { shouldDirty: true, shouldValidate: true }
  )
}

function removeChannelPrice(form: ChannelForm, model: string) {
  const next = { ...form.getValues('model_prices') }
  delete next[model]
  form.setValue('model_prices', next, {
    shouldDirty: true,
    shouldValidate: true,
  })
}

function buildSitePricedModelSet(
  models: Array<{ model_name: string; pricing_available?: boolean }>
): Set<string> {
  return new Set(
    models
      .filter((model) => model.pricing_available !== false)
      .map((model) => modelKey(model.model_name))
  )
}
