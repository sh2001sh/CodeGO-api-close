import { useMemo } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { AlertTriangle, Check, CircleDollarSign } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import type { ChannelFormInput } from '../lib/channel-form'
import { modelKey } from '../lib/channel-model-sync'
import type { ChannelModelPrice } from '../types'
import { ChannelPriceEditor } from './channel-model-price-editor'

type ChannelForm = UseFormReturn<ChannelFormInput>

export function ChannelModelPrices(props: {
  form: ChannelForm
  selectedModels: string[]
}) {
  const { t } = useTranslation()
  const pricing = usePricingData()
  const prices = props.form.watch('model_prices')
  const channelPrices = useMemo(
    () =>
      new Map(
        Object.entries(prices).map(([model, price]) => [modelKey(model), price])
      ),
    [prices]
  )
  const sitePricedModels = useMemo(
    () => buildSitePricedModelSet(pricing.models, pricing.pricedModels),
    [pricing.models, pricing.pricedModels]
  )
  if (props.selectedModels.length === 0) return null

  const customModels = props.selectedModels.filter((model) =>
    channelPrices.has(modelKey(model))
  )
  const missingModels = pricing.isLoading
    ? []
    : props.selectedModels.filter(
        (model) =>
          !sitePricedModels.has(modelKey(model)) &&
          !channelPrices.has(modelKey(model))
      )
  const editableModels = props.selectedModels.filter(
    (model) =>
      channelPrices.has(modelKey(model)) ||
      !sitePricedModels.has(modelKey(model))
  )
  const siteCoveredModels = props.selectedModels.filter(
    (model) =>
      sitePricedModels.has(modelKey(model)) &&
      !channelPrices.has(modelKey(model))
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
              price={channelPrices.get(modelKey(model))}
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
        key={`${props.model}:${JSON.stringify(props.price ?? null)}`}
        price={props.price}
        onSave={props.onSave}
        onRemove={props.onRemove}
      />
    </div>
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
  models: Array<{ model_name: string; pricing_available?: boolean }>,
  pricedModels: string[]
): Set<string> {
  const result = new Set(
    models
      .filter((model) => model.pricing_available !== false)
      .map((model) => modelKey(model.model_name))
  )
  for (const model of pricedModels) result.add(modelKey(model))
  return result
}
