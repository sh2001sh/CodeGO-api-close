import { expect, it } from 'vitest'
import { matchesSearch } from './search'
import { fieldByKey } from './registry'
import english from '../../locales/en'

it('finds settings by their displayed translation and preserves source and configuration-key lookup', () => {
  const field = fieldByKey('EmailVerificationEnabled')!
  const dictionary: Record<string, string> = english
  const t = (key: string) => dictionary[key] ?? key
  expect(matchesSearch(field, 'Registration email verification code', t)).toBe(true)
  expect(matchesSearch(field, 'EMAILVERIFICATION', t)).toBe(true)
  expect(matchesSearch(field, '注册邮箱', t)).toBe(true)
  expect(matchesSearch(field, 'unknown setting', t)).toBe(false)
})
