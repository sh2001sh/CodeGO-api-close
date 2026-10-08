import { describe, expect, it } from 'vitest'
import { languages, preferredLocale, resolveLocale } from './locales'

describe('locale resolution', () => {
  it('keeps saved preferences ahead of browser detection', () => {
    expect(preferredLocale('en', ['zh-HK'])).toBe('en')
    expect(preferredLocale('zh-CN', ['en-US'])).toBe('zh-CN')
    expect(preferredLocale('zh-TW', ['en'])).toBe('zh-HK')
  })
  it('recognizes regional and script variants and uses Hong Kong for unsupported browsers', () => {
    expect(resolveLocale('zh-Hant')).toBe('zh-HK')
    expect(resolveLocale('zh_HK')).toBe('zh-HK')
    expect(resolveLocale('zh-Hans-SG')).toBe('zh-CN')
    expect(resolveLocale('ar-SA')).toBe('ar')
    expect(preferredLocale('unsupported', ['es', 'ja-JP'])).toBe('ja')
    expect(preferredLocale(null, ['es'])).toBe('zh-HK')
    expect(resolveLocale('not-a-language')).toBeUndefined()
    expect(languages).toHaveLength(9)
  })
})
