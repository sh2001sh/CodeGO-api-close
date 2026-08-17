import { z } from 'zod'
import type { MarketplaceChannel } from '../types'

export const MARKETPLACE_SOURCE_OPTIONS = [
  'Codex Plus',
  'Codex Pro',
  'CC-Max',
  'CC-Kiro',
  'CC其它',
  '国产模型',
] as const

export const channelFormSchema = z.object({
  provider_type: z.enum([
    'openai_compatible',
    'codex',
    'azure_openai',
    'anthropic',
    'gemini',
  ]),
  source_label: z.enum(MARKETPLACE_SOURCE_OPTIONS),
  base_url: z.string().url().startsWith('https://'),
  api_key: z.string().min(1),
  declared_models: z.array(z.string()).min(1),
  model_prices: z.record(
    z.string(),
    z.object({
      input_price_per_million: z.number().finite().positive().max(1_000_000),
      output_price_per_million: z.number().finite().positive().max(1_000_000),
    })
  ),
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

export const channelEditFormSchema = channelFormSchema.extend({
  base_url: z.union([z.literal(''), z.string().url().startsWith('https://')]),
  api_key: z.string(),
})

export const channelFormDefaults: ChannelFormInput = {
  provider_type: 'openai_compatible',
  source_label: 'Codex Plus',
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
  auto_probe_enabled: false,
  auto_probe_interval_minutes: 10,
  auto_probe_model: '',
  model_consistency_status: 'none',
}

export function channelFormDefaultsForEdit(
  channel: MarketplaceChannel
): ChannelFormInput {
  const source = MARKETPLACE_SOURCE_OPTIONS.includes(
    channel.submitted_source_label as (typeof MARKETPLACE_SOURCE_OPTIONS)[number]
  )
    ? (channel.submitted_source_label as ChannelFormInput['source_label'])
    : 'CC其它'
  return {
    provider_type: channel.provider_type as ChannelFormInput['provider_type'],
    source_label: source,
    base_url: '',
    api_key: '',
    declared_models: channel.declared_models,
    model_prices: channel.model_prices ?? {},
    multiplier: channel.multiplier,
    visibility: channel.visibility as ChannelFormInput['visibility'],
    max_concurrency: channel.max_concurrency,
    user_max_concurrency: channel.user_max_concurrency ?? 0,
    qps: channel.qps,
    maintenance_window: channel.maintenance_window,
    sensitive_word_interception_enabled:
      channel.sensitive_word_interception_enabled,
    auto_probe_enabled: channel.auto_probe_enabled,
    auto_probe_interval_minutes: channel.auto_probe_interval_minutes || 10,
    auto_probe_model:
      channel.auto_probe_model || channel.declared_models[0] || '',
    model_consistency_status: channel.model_consistency_status || 'none',
  }
}
