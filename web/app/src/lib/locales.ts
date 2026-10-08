export const languages = [
  { code: 'zh-HK', name: '繁體中文（香港）', short: '繁', dir: 'ltr' },
  { code: 'zh-CN', name: '简体中文', short: '简', dir: 'ltr' },
  { code: 'en', name: 'English', short: 'EN', dir: 'ltr' },
  { code: 'ja', name: '日本語', short: '日本', dir: 'ltr' },
  { code: 'ru', name: 'Русский', short: 'RU', dir: 'ltr' },
  { code: 'ko', name: '한국어', short: '한국', dir: 'ltr' },
  { code: 'fr', name: 'Français', short: 'FR', dir: 'ltr' },
  { code: 'de', name: 'Deutsch', short: 'DE', dir: 'ltr' },
  { code: 'ar', name: 'العربية', short: 'AR', dir: 'rtl' },
] as const

export type Locale = (typeof languages)[number]['code']

export function resolveLocale(value: string | null | undefined): Locale | undefined {
  if (!value) return undefined
  const tag = value.toLowerCase().replaceAll('_', '-')
  if (tag.startsWith('zh')) {
    return /hant|tw|hk|mo/.test(tag) ? 'zh-HK' : 'zh-CN'
  }
  return languages.find((language) => tag.split('-')[0] === language.code)?.code
}

export function preferredLocale(saved: string | null, browserLanguages: readonly string[]): Locale {
  return resolveLocale(saved) ?? browserLanguages.map(resolveLocale).find(Boolean) ?? 'zh-HK'
}
