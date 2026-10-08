import { describe, expect, it } from 'vitest'
import { disclosureInput } from './disclosure-editor'

function fields(models: readonly string[] = ['model-a'], prefix = 'disclosure') {
  const body = new FormData()
  const set = (key: string, value: string) => body.set(`${prefix}-${key}`, value)
  set('source', 'unknown')
  set('retention', 'unknown')
  set('training', 'unknown')
  models.forEach((_model, index) => {
    for (const feature of ['streaming', 'tools', 'structured_outputs', 'vision'])
      set(`model-${index}-${feature}`, 'unknown')
  })
  return { body, set }
}

describe('channel owner disclosure input', () => {
  it('preserves unknown claims rather than declaring capabilities or zero limits', () => {
    const { body } = fields()
    expect(disclosureInput(body, ['model-a'])).toEqual({
      source_kind: 'unknown',
      retention: 'unknown',
      training: 'unknown',
      regions: [],
      models: [
        {
          model: 'model-a',
          streaming: 'unknown',
          tools: 'unknown',
          structured_outputs: 'unknown',
          vision: 'unknown',
          context_tokens: undefined,
          max_output_tokens: undefined,
        },
      ],
    })
  })

  it('serializes model limits as integers and isolates prefixed editor fields', () => {
    const { body, set } = fields(['model-a', 'model-b'], 'instance-7')
    set('regions', 'hk, US；hk jp')
    set('source', 'reseller')
    set('retention', 'limited')
    set('retention-days', '30')
    set('training', 'no')
    set('policy-url', 'https://example.com/privacy')
    set('model-0-tools', 'supported')
    set('model-0-context', '1000000000')
    set('model-1-output', '1024')
    const result = disclosureInput(body, ['model-a', 'model-b'], 'instance-7')
    expect(result.regions).toEqual(['HK', 'US', 'JP'])
    expect(result.retention_days).toBe(30)
    expect(result.models[0].context_tokens).toBe(1000000000n)
    expect(result.models[0].tools).toBe('supported')
    expect(result.models[1].max_output_tokens).toBe(1024n)
    expect(result.models[1].tools).toBe('unknown')
  })

  it('removes stale retention and never accepts claimed verification metadata', () => {
    const { body, set } = fields()
    set('retention', 'none')
    set('retention-days', '30')
    set('provenance', 'verified')
    set('updated_at', '2026-10-07T00:00:00Z')
    const result = disclosureInput(body, ['model-a'])
    expect(result).not.toHaveProperty('retention_days')
    expect(result).not.toHaveProperty('provenance')
    expect(result).not.toHaveProperty('updated_at')
  })

  it('rejects invalid choices, countries, retention boundaries and credential URLs', () => {
    for (const [key, value] of [
      ['source', 'official_verified'],
      ['regions', 'Hong Kong'],
      ['policy-url', 'http://example.com/privacy'],
      ['policy-url', 'https://secret@example.com/privacy'],
      ['policy-url', 'javascript:alert(1)'],
      ['model-0-streaming', 'certified'],
      ['model-0-context', '0'],
      ['model-0-context', '1.5'],
      ['model-0-context', '1000000001'],
      ['model-0-context', '9223372036854775808'],
    ]) {
      const { body, set } = fields()
      set(key, value)
      expect(() => disclosureInput(body, ['model-a'])).toThrow()
    }
    for (const days of ['-1', '3651', '1.5']) {
      const { body, set } = fields()
      set('retention', 'limited')
      set('retention-days', days)
      expect(() => disclosureInput(body, ['model-a'])).toThrow()
    }
    for (const days of ['', '0', '3650']) {
      const { body, set } = fields()
      set('retention', 'limited')
      set('retention-days', days)
      expect(() => disclosureInput(body, ['model-a'])).not.toThrow()
    }
    const { body, set } = fields()
    set('model-0-context', '100')
    set('model-0-output', '101')
    expect(() => disclosureInput(body, ['model-a'])).toThrow('输出上限不能超过上下文上限')
  })
})
