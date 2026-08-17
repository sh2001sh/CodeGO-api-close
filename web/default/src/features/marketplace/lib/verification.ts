const GPT56_MODELS = new Set(['gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna'])

/** Returns whether the declared model set requires GPT-5.6 mapping detection. */
export function hasGPT56Model(models: string[]) {
  return models.some((model) => GPT56_MODELS.has(model.trim().toLowerCase()))
}
