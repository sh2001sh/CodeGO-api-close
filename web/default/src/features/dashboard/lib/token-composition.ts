type TokenLog = {
  prompt_tokens?: number
  completion_tokens?: number
  other?: string
}

function nonnegative(value: unknown): number {
  const number = Number(value)
  return Number.isFinite(number) ? Math.max(0, number) : 0
}

export function getTokenComposition(logs: readonly TokenLog[]) {
  const totals = { cacheHit: 0, cacheMiss: 0, output: 0 }
  for (const log of logs) {
    let input = nonnegative(log.prompt_tokens)
    let details: Record<string, unknown>
    try {
      details = JSON.parse(log.other || '{}') ?? {}
    } catch {
      // Missing/invalid cache metadata cannot establish a cache hit.
      details = {}
    }
    const cached = nonnegative(details.cache_tokens)
    if (details.usage_semantic === 'anthropic' || details.claude === true) {
      // Native Anthropic logs store uncached input separately from cache reads/writes.
      const created = Math.max(
        nonnegative(details.cache_write_tokens),
        nonnegative(details.cache_creation_tokens),
        nonnegative(details.cache_creation_tokens_5m) +
          nonnegative(details.cache_creation_tokens_1h)
      )
      input += cached + created
    } else if (nonnegative(details.input_tokens_total) > 0) {
      input = nonnegative(details.input_tokens_total)
    }
    // OpenAI input is inclusive; clamp per request before summing.
    const hit = Math.min(input, cached)
    totals.cacheHit += hit
    totals.cacheMiss += input - hit
    totals.output += nonnegative(log.completion_tokens)
  }
  return totals
}
