export function normalizeFullApiKey(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  if (trimmed.startsWith('sk-sk-')) return trimmed.slice(3)
  if (trimmed.startsWith('sk-')) return trimmed
  return `sk-${trimmed}`
}
