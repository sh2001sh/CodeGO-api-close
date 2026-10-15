import type { ChannelModelPrice } from '../types'

export type ChannelBillingMode = 'token' | 'per_call'

export interface ChannelPriceDraft {
  mode: ChannelBillingMode
  pricePerCall: string
  inputPrice: string
  outputPrice: string
  cacheReadEnabled: boolean
  cacheReadPrice: string
  cacheWriteEnabled: boolean
  cacheWritePrice: string
}

export function channelPriceToDraft(
  price?: ChannelModelPrice
): ChannelPriceDraft {
  return {
    mode: price?.billing_mode === 'per_call' ? 'per_call' : 'token',
    pricePerCall: numberToDraft(price?.price_per_call),
    inputPrice: numberToDraft(price?.input_price_per_million),
    outputPrice: numberToDraft(price?.output_price_per_million),
    cacheReadEnabled: price?.cache_read_price_per_million !== undefined,
    cacheReadPrice: numberToDraft(price?.cache_read_price_per_million),
    cacheWriteEnabled: price?.cache_write_price_per_million !== undefined,
    cacheWritePrice: numberToDraft(price?.cache_write_price_per_million),
  }
}

export function validateChannelPriceDraft(
  draft: ChannelPriceDraft,
  t: (value: string) => string
): string {
  if (draft.mode === 'per_call') {
    return validatePositivePrice(draft.pricePerCall, t('每次请求价格'), t)
  }
  const inputError = validatePositivePrice(draft.inputPrice, t('输入价格'), t)
  if (inputError) return inputError
  const outputError = validatePositivePrice(draft.outputPrice, t('输出价格'), t)
  if (outputError) return outputError
  if (draft.cacheReadEnabled) {
    const error = validateOptionalPrice(
      draft.cacheReadPrice,
      t('缓存读取价格'),
      t
    )
    if (error) return error
  }
  if (draft.cacheWriteEnabled) {
    return validateOptionalPrice(draft.cacheWritePrice, t('缓存写入价格'), t)
  }
  return ''
}

export function channelPriceDraftToValue(
  draft: ChannelPriceDraft
): ChannelModelPrice {
  if (draft.mode === 'per_call') {
    return {
      billing_mode: 'per_call',
      price_per_call: Number(draft.pricePerCall),
    }
  }
  return {
    billing_mode: 'token',
    input_price_per_million: Number(draft.inputPrice),
    output_price_per_million: Number(draft.outputPrice),
    ...(draft.cacheReadEnabled
      ? { cache_read_price_per_million: Number(draft.cacheReadPrice) }
      : {}),
    ...(draft.cacheWriteEnabled
      ? { cache_write_price_per_million: Number(draft.cacheWritePrice) }
      : {}),
  }
}

function numberToDraft(value?: number): string {
  return value === undefined ? '' : String(value)
}

function validatePositivePrice(
  raw: string,
  label: string,
  t: (value: string) => string
): string {
  const value = Number(raw)
  if (!raw || !Number.isFinite(value) || value <= 0)
    return `${label}${t('必须大于 0')}`
  if (value > 1_000_000) return `${label}${t('不能超过 1000000')}`
  return ''
}

function validateOptionalPrice(
  raw: string,
  label: string,
  t: (value: string) => string
): string {
  const value = Number(raw)
  if (!raw || !Number.isFinite(value) || value < 0)
    return `${label}${t('必须大于或等于 0')}`
  if (value > 1_000_000) return `${label}${t('不能超过 1000000')}`
  return ''
}
