import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { isImageGenerationModel } from './model-capabilities'

describe('marketplace model capabilities', () => {
  test('recognizes Grok image generation variants', () => {
    for (const model of [
      'grok-imagine-image',
      'grok-imagine-image-2.0',
      'grok-imagine-image-quality-lite',
      'grok-2-image-1212',
    ]) {
      assert.equal(isImageGenerationModel(model), true, model)
    }
  })

  test('does not classify Grok chat and vision models as image generation', () => {
    assert.equal(isImageGenerationModel('grok-2-vision-1212'), false)
    assert.equal(isImageGenerationModel('grok-4.5'), false)
  })
})
