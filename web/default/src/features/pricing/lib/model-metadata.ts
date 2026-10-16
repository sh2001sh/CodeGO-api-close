/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { getIntlLocale } from '@/i18n/format'
import type { Modality, ModelCapability, PricingModel } from '../types'

export type ModelMetadata = {
  context_length: number
  max_output_tokens: number
  knowledge_cutoff: string
  release_date: string
  parameter_count: string
  input_modalities: Modality[]
  output_modalities: Modality[]
  capabilities: ModelCapability[]
}

/** Missing fields stay unknown; model names never establish capability or policy. */
export function inferModelMetadata(model: PricingModel): ModelMetadata {
  return {
    context_length: model.context_length ?? 0,
    max_output_tokens: model.max_output_tokens ?? 0,
    knowledge_cutoff: model.knowledge_cutoff ?? '',
    release_date: model.release_date ?? '',
    parameter_count: model.parameter_count ?? '',
    input_modalities: model.input_modalities ?? [],
    output_modalities: model.output_modalities ?? [],
    capabilities: model.capabilities ?? [],
  }
}

const tokenFormatter = () =>
  new Intl.NumberFormat(getIntlLocale(), {
    maximumFractionDigits: 1,
  })

/** Format a token count compactly: 128_000 → "128K", 1_000_000 → "1M". */
export function formatTokenCount(tokens: number): string {
  if (!Number.isFinite(tokens) || tokens <= 0) return '—'
  if (tokens >= 1_000_000) {
    const value = tokens / 1_000_000
    return `${tokenFormatter().format(value)}M`
  }
  if (tokens >= 1_000) {
    const value = tokens / 1_000
    return `${tokenFormatter().format(value)}K`
  }
  return tokenFormatter().format(tokens)
}

/** Format a YYYY-MM (or YYYY-MM-DD) date as `Mon YYYY` for display. */
export function formatYearMonth(value: string): string {
  if (!value) return '—'
  const [yearStr, monthStr] = value.split('-')
  const year = Number(yearStr)
  const month = Number(monthStr)
  if (!Number.isFinite(year) || !Number.isFinite(month)) return value
  const date = new Date(Date.UTC(year, month - 1, 1))
  return date.toLocaleString(getIntlLocale(), {
    year: 'numeric',
    month: 'short',
  })
}

export type ModelVendor =
  | 'openai'
  | 'anthropic'
  | 'google'
  | 'meta'
  | 'mistral'
  | 'qwen'
  | 'deepseek'
  | 'xai'
  | 'cohere'
  | 'baidu'
  | 'zhipu'
  | 'moonshot'
  | 'minimax'
  | 'tencent'
  | 'bytedance'
  | 'stability'
  | 'unknown'

export type ApiInfo = {
  vendor: ModelVendor
  vendor_label: string
  tokenizer: string
  tokenizer_note?: string
  license: string
  license_kind: 'proprietary' | 'open' | 'open-weight' | 'unknown'
  data_retention_days: number | null
  training_opt_out: boolean | null
  homepage?: string
}

const VENDOR_LABELS: Record<ModelVendor, string> = {
  openai: 'OpenAI',
  anthropic: 'Anthropic',
  google: 'Google',
  meta: 'Meta',
  mistral: 'Mistral AI',
  qwen: 'Alibaba (Qwen)',
  deepseek: 'DeepSeek',
  xai: 'xAI',
  cohere: 'Cohere',
  baidu: 'Baidu',
  zhipu: 'Zhipu AI',
  moonshot: 'Moonshot AI',
  minimax: 'MiniMax',
  tencent: 'Tencent',
  bytedance: 'ByteDance',
  stability: 'Stability AI',
  unknown: 'Unknown',
}

function detectVendor(name: string): ModelVendor {
  const n = name.toLowerCase()
  if (/^gpt|^o[1-4]|davinci|babbage|whisper|tts|dall.?e|^omni/.test(n))
    return 'openai'
  if (/claude/.test(n)) return 'anthropic'
  if (/gemini|gemma|imagen|veo|palm/.test(n)) return 'google'
  if (/llama|^codellama/.test(n)) return 'meta'
  if (/mistral|mixtral|codestral|magistral|pixtral/.test(n)) return 'mistral'
  if (/qwen|qwq|qvq/.test(n)) return 'qwen'
  if (/deepseek/.test(n)) return 'deepseek'
  if (/grok/.test(n)) return 'xai'
  if (/command|cohere|aya/.test(n)) return 'cohere'
  if (/ernie|wenxin/.test(n)) return 'baidu'
  if (/glm|chatglm|cogview|cogvideo/.test(n)) return 'zhipu'
  if (/kimi|moonshot/.test(n)) return 'moonshot'
  if (/abab|minimax|hailuo/.test(n)) return 'minimax'
  if (/hunyuan/.test(n)) return 'tencent'
  if (/doubao|seed|jimeng/.test(n)) return 'bytedance'
  if (/^sd-|stable[-_]?diffusion|sdxl/.test(n)) return 'stability'
  return 'unknown'
}

const HOMEPAGE_BY_VENDOR: Partial<Record<ModelVendor, string>> = {
  openai: 'https://platform.openai.com/docs/models',
  anthropic: 'https://docs.anthropic.com/claude/docs/models-overview',
  google: 'https://ai.google.dev/models',
  meta: 'https://llama.meta.com/',
  mistral: 'https://docs.mistral.ai/getting-started/models/',
  qwen: 'https://qwenlm.github.io/',
  deepseek: 'https://api-docs.deepseek.com/',
  xai: 'https://x.ai/api',
  cohere: 'https://docs.cohere.com/docs/models',
  baidu: 'https://cloud.baidu.com/product/wenxinworkshop',
  zhipu: 'https://open.bigmodel.cn/dev/api',
  moonshot: 'https://platform.moonshot.cn/docs',
  minimax: 'https://platform.minimaxi.com/document/notice',
  tencent: 'https://cloud.tencent.com/document/product/1729',
  bytedance: 'https://www.volcengine.com/docs/82379',
  stability: 'https://platform.stability.ai/',
}

/** Vendor links are references; tokenizer, license and processing policies need evidence. */
export function inferApiInfo(model: PricingModel): ApiInfo {
  const vendor = detectVendor(model.model_name || '')
  return {
    vendor,
    vendor_label: model.vendor_name || VENDOR_LABELS[vendor],
    tokenizer: 'Unknown',
    license: 'Unknown',
    license_kind: 'unknown',
    data_retention_days: null,
    training_opt_out: null,
    homepage: HOMEPAGE_BY_VENDOR[vendor],
  }
}
