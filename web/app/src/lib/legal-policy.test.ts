import { describe, expect, it } from 'vitest'
import { currentPolicyVersion, hasCurrentPolicyAcceptance } from './legal-policy'

describe('explicit policy acceptance', () => {
  it('does not infer supplier acceptance from other documents or older versions', () => {
    expect(hasCurrentPolicyAcceptance([], 'supplier')).toBe(false)
    expect(
      hasCurrentPolicyAcceptance(
        [
          { document: 'terms', version: currentPolicyVersion },
          { document: 'supplier', version: '2026-10-04' },
        ],
        'supplier',
      ),
    ).toBe(false)
    expect(
      hasCurrentPolicyAcceptance(
        [{ document: 'supplier', version: currentPolicyVersion }],
        'supplier',
      ),
    ).toBe(true)
  })
})
