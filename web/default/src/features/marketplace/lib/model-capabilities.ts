/** Returns whether a model uses the image-generation endpoint and per-call pricing. */
export function isImageGenerationModel(model: string): boolean {
  const normalized = model.trim().toLowerCase()
  const prefixes = [
    'dall-e-',
    'gpt-image-',
    'imagen-',
    'grok-imagine-image',
    'grok-2-image',
  ]
  const fragments = ['flux-', 'flux.1-', 'image-generation']

  return (
    prefixes.some((prefix) => normalized.startsWith(prefix)) ||
    fragments.some((fragment) => normalized.includes(fragment))
  )
}
