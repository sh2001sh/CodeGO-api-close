import { z } from 'zod'
import interfaceI18n from '@/i18n/config'
import type { MarketplaceChannel } from '../types'

const sourceLinkPattern =
  /([a-z][a-z0-9+.-]*:\/\/|(?:https?|hxxps?|mailto|javascript|data):|www\.|(?:[\p{L}\p{N}][\p{L}\p{N}-]*\.)+(?:[\p{L}]{2,}|xn--[a-z0-9-]+)|[0-9]{1,3}(?:\.[0-9]{1,3}){3}|(?:[0-9a-f]{0,4}:){2,}[0-9a-f]{0,4})/iu

export const MARKETPLACE_SOURCE_OPTIONS = [
  'Codex Plus',
  'Codex Pro',
  'Codex 混合号池',
  'Grok',
  'Gemini',
  'CC-Max',
  'CC-Kiro',
  'CC其它',
  '国产模型',
] as const

const optionalChannelPrice = z.number().finite().nonnegative().max(1_000_000)

export const channelModelPriceSchema = z
  .object({
    billing_mode: z.enum(['token', 'per_call']).optional(),
    price_per_call: optionalChannelPrice.optional(),
    input_price_per_million: optionalChannelPrice.optional(),
    output_price_per_million: optionalChannelPrice.optional(),
    cache_read_price_per_million: optionalChannelPrice.optional(),
    cache_write_price_per_million: optionalChannelPrice.optional(),
  })
  .superRefine((price, context) => {
    const mode = price.billing_mode ?? 'token'
    if (mode === 'per_call') {
      if (!price.price_per_call || price.price_per_call <= 0) {
        context.addIssue({
          code: 'custom',
          path: ['price_per_call'],
          message: interfaceI18n.t('每次请求价格必须大于 0'),
        })
      }
      return
    }
    if (!price.input_price_per_million || price.input_price_per_million <= 0) {
      context.addIssue({
        code: 'custom',
        path: ['input_price_per_million'],
        message: interfaceI18n.t('输入价格必须大于 0'),
      })
    }
    if (
      !price.output_price_per_million ||
      price.output_price_per_million <= 0
    ) {
      context.addIssue({
        code: 'custom',
        path: ['output_price_per_million'],
        message: interfaceI18n.t('输出价格必须大于 0'),
      })
    }
  })

export const channelFormSchema = z.object({
  custom_name: z.string().max(20, {
    error: () => interfaceI18n.t('标题最多 20 字，详细说明请填写下方备注'),
  }),
  description: z.string().max(300),
  tags: z.array(z.string().min(1).max(16)).max(6),
  provider_type: z.enum([
    'openai_compatible',
    'codex',
    'azure_openai',
    'anthropic',
    'gemini',
  ]),
  source_label: z
    .string()
    .transform((value) => value.normalize('NFKC').trim())
    .refine((value) => value.length > 0, {
      error: () => interfaceI18n.t('请填写来源声明'),
    })
    .refine((value) => Array.from(value).length <= 30, {
      error: () => interfaceI18n.t('来源声明最多 30 个字符'),
    })
    .refine(
      (value) =>
        !sourceLinkPattern.test(value.replace(/[^\p{L}\p{N}.:@/+-]/gu, '')),
      {
        error: () => interfaceI18n.t('来源声明不能包含链接、域名或 IP 地址'),
      }
    )
    .refine((value) => !/[\p{Cc}\p{Cf}]/u.test(value), {
      error: () => interfaceI18n.t('来源声明不能包含换行或隐藏字符'),
    }),
  base_url: z.url({ protocol: /^https?$/ }),
  api_key: z.string().min(1),
  declared_models: z.array(z.string()).min(1),
  model_prices: z.record(z.string(), channelModelPriceSchema),
  multiplier: z
    .number()
    .finite()
    .positive('倍率必须大于 0')
    .max(1_000_000, '倍率不能超过 1000000x'),
  visibility: z.enum(['private', 'unlisted', 'public']),
  max_concurrency: z.number().int().min(0).max(10000),
  user_max_concurrency: z.number().int().min(0).max(10000),
  qps: z.number().positive().max(10000),
  maintenance_window: z.string().max(255),
  sensitive_word_interception_enabled: z.boolean(),
  multiplier_card_supported: z.boolean(),
  multiplier_card_user_enabled: z.boolean(),
  credit_pack_supported: z.boolean(),
  security_alert_email_enabled: z.boolean(),
  auto_probe_enabled: z.boolean(),
  auto_probe_interval_minutes: z.number().int().min(1).max(1440),
  auto_probe_model: z.string().max(128),
  model_consistency_status: z.enum([
    'none',
    'passed',
    'failed',
    'questionable',
  ]),
})

export type ChannelFormInput = z.infer<typeof channelFormSchema>

export const channelPresentationSchema = channelFormSchema.pick({
  custom_name: true,
  description: true,
  tags: true,
})

export type ChannelPresentationInput = z.infer<typeof channelPresentationSchema>

export const channelEditFormSchema = channelFormSchema.extend({
  base_url: z.union([z.literal(''), channelFormSchema.shape.base_url]),
  api_key: z.string(),
})

export const channelFormDefaults: ChannelFormInput = {
  custom_name: '',
  description: '',
  tags: [],
  provider_type: 'openai_compatible',
  source_label: '',
  base_url: '',
  api_key: '',
  declared_models: [],
  model_prices: {},
  multiplier: 1,
  visibility: 'public',
  max_concurrency: 10,
  user_max_concurrency: 0,
  qps: 5,
  maintenance_window: '',
  sensitive_word_interception_enabled: true,
  multiplier_card_supported: false,
  multiplier_card_user_enabled: false,
  credit_pack_supported: false,
  security_alert_email_enabled: false,
  auto_probe_enabled: false,
  auto_probe_interval_minutes: 10,
  auto_probe_model: '',
  model_consistency_status: 'none',
}

export function channelFormDefaultsForEdit(
  channel: MarketplaceChannel
): ChannelFormInput {
  const source =
    channel.submitted_source_label || channel.approved_source_label || ''
  const declaredModels = channel.declared_models ?? []
  return {
    custom_name: channel.custom_name ?? '',
    description: channel.description ?? '',
    tags: channel.tags ?? [],
    provider_type: (channel.provider_type ||
      channelFormDefaults.provider_type) as ChannelFormInput['provider_type'],
    source_label: source,
    base_url: '',
    api_key: '',
    declared_models: declaredModels,
    model_prices: channel.model_prices ?? {},
    multiplier:
      channel.multiplier > 0
        ? channel.multiplier
        : channelFormDefaults.multiplier,
    visibility: (channel.visibility ||
      channelFormDefaults.visibility) as ChannelFormInput['visibility'],
    max_concurrency:
      channel.max_concurrency ?? channelFormDefaults.max_concurrency,
    user_max_concurrency: channel.user_max_concurrency ?? 0,
    qps: channel.qps > 0 ? channel.qps : channelFormDefaults.qps,
    maintenance_window: channel.maintenance_window ?? '',
    sensitive_word_interception_enabled:
      channel.sensitive_word_interception_enabled ??
      channelFormDefaults.sensitive_word_interception_enabled,
    multiplier_card_supported: channel.multiplier_card_supported ?? false,
    multiplier_card_user_enabled: channel.multiplier_card_user_enabled ?? false,
    credit_pack_supported: channel.credit_pack_supported ?? false,
    security_alert_email_enabled: channel.security_alert_email_enabled ?? false,
    auto_probe_enabled:
      channel.auto_probe_enabled ?? channelFormDefaults.auto_probe_enabled,
    auto_probe_interval_minutes:
      channel.auto_probe_interval_minutes > 0
        ? channel.auto_probe_interval_minutes
        : channelFormDefaults.auto_probe_interval_minutes,
    auto_probe_model: channel.auto_probe_model || declaredModels[0] || '',
    model_consistency_status: channel.model_consistency_status || 'none',
  }
}
