// Pure matching helper for the settings search box: matches by label or key,
// case-insensitively, across all registered fields.
import type { SettingFieldDef } from './registry-types'

export function matchesSearch(
  field: SettingFieldDef,
  query: string,
  t = (key: string) => key,
): boolean {
  const needle = query.trim().toLowerCase()
  if (!needle) return true
  return (
    field.label.toLowerCase().includes(needle) ||
    t(field.label).toLowerCase().includes(needle) ||
    field.key.toLowerCase().includes(needle) ||
    (field.description?.toLowerCase().includes(needle) ?? false) ||
    (field.description ? t(field.description).toLowerCase().includes(needle) : false)
  )
}
