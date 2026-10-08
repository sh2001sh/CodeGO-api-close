// Shared types and constants for the catalog-admin workstream (channels,
// model catalog, vendors, prefill groups). All network calls use the
// `/api/catalog/*` family, which is the authoritative, current naming for
// catalog metadata; `/api/models`, `/api/vendors` and `/api/prefill_group`
// are kept server-side only as legacy path aliases to the same handlers.
// Channel batch/tag/test/fetch-models operate on `/api/channel/*` because
// those capabilities have no equivalent under `/api/catalog/channels`.
import type { Schema } from '../../lib/types'

export type Channel = Schema['CatalogChannel']
export type ChannelInput = Schema['CatalogChannelInput']
export type ProbeResult = Schema['CatalogProbeResult']
export type ProbeBatch = Schema['CatalogProbeBatch']
export type ModelView = Schema['CatalogModelView']
export type VendorMetadata = Schema['CatalogVendorMetadata']
export type PrefillGroup = Schema['CatalogPrefillGroup']
export type SyncPreview = Schema['CatalogMetadataSyncPreview']
export type SyncResult = Schema['CatalogMetadataSyncResult']
export type MetadataConflict = Schema['CatalogMetadataConflict']

// Mirrors v3/internal/catalogcontrol/legacy_channels.go legacyProviderIDs().
// Required by the legacy /api/channel/fetch_models endpoint, which still
// identifies providers by their historical numeric channel type.
const LEGACY_PROVIDER_IDS: Record<string, number> = {
  openai: 1,
  azure: 3,
  ollama: 4,
  openaimax: 6,
  ohmygpt: 7,
  custom: 8,
  ails: 9,
  aiproxy: 10,
  palm: 11,
  api2gpt: 12,
  aigc2d: 13,
  claude: 14,
  baidu: 15,
  zhipu: 16,
  ali: 17,
  xunfei: 18,
  '360': 19,
  openrouter: 20,
  aiproxy_library: 21,
  fastgpt: 22,
  tencent: 23,
  gemini: 24,
  moonshot: 25,
  zhipu_v4: 26,
  perplexity: 27,
  lingyiwanwu: 31,
  aws: 33,
  cohere: 34,
  minimax: 35,
  suno: 36,
  dify: 37,
  jina: 38,
  cloudflare: 39,
  siliconflow: 40,
  vertex: 41,
  mistral: 42,
  deepseek: 43,
  mokaai: 44,
  volcengine: 45,
  baidu_v2: 46,
  xinference: 47,
  xai: 48,
  coze: 49,
  kling: 50,
  jimeng: 51,
  vidu: 52,
  submodel: 53,
  doubao_video: 54,
  replicate: 56,
  codex: 57,
}

export function legacyProviderType(provider: string): number | undefined {
  return LEGACY_PROVIDER_IDS[provider.trim().toLowerCase()]
}
