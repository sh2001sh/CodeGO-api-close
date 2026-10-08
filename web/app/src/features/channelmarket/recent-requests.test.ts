import { expect, it } from 'vitest'
import { requestTone } from './recent-requests'
it('classifies real hourly requests at exact thresholds, including no samples', () => {
  expect(requestTone('9223372036854775807', 90.01)).toBe('good')
  expect(requestTone(1, 90)).toBe('warning')
  expect(requestTone(1, 75)).toBe('warning')
  expect(requestTone(1, 74.99)).toBe('poor')
  expect(requestTone(0, 100)).toBe('empty')
})
