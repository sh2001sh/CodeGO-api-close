/** Stable platform vocabulary; labels are translated at the point of display. */
export const marketTags = [
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'google', label: 'Google' },
  { value: 'deepseek', label: 'DeepSeek' },
  { value: 'xai', label: 'xAI' },
  { value: 'meta', label: 'Meta' },
  { value: 'mistral', label: 'Mistral' },
  { value: 'qwen', label: 'Qwen' },
  { value: 'moonshot', label: 'Moonshot' },
  { value: 'zhipu', label: 'Zhipu' },
  { value: 'cohere', label: 'Cohere' },
] as const

export const marketTagLabel = (tag: string): string =>
  marketTags.find((item) => item.value === tag)?.label ?? '其他'

/** Recommend only within the caller's already authorized market listing. */
export function relatedGroups(
  groups: readonly PublicGroup[],
  selected: PublicGroup,
  model: string,
) {
  const selectedModels = new Set(selected.declared_models ?? [])
  const selectedTags = new Set(selected.tags ?? [])
  return groups
    .filter(
      (group) =>
        group.group_id !== selected.group_id && (!model || group.declared_models?.includes(model)),
    )
    .map((group) => ({
      group,
      score:
        (group.tags ?? []).filter((tag) => selectedTags.has(tag)).length * 2 +
        Number((group.declared_models ?? []).some((name) => selectedModels.has(name))),
    }))
    .filter((item) => item.score > 0)
    .sort(
      (a, b) =>
        b.score - a.score || a.group.system_display_name.localeCompare(b.group.system_display_name),
    )
    .slice(0, 3)
    .map((item) => item.group)
}
import type { PublicGroup } from '../../lib/public-catalog'
