import i18next from 'i18next'
import { toIntlLocale } from './languages'

export function getIntlLocale(): string {
  return toIntlLocale(i18next.language) ?? 'zh-HK'
}
