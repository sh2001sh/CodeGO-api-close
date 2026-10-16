/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'
import {
  INTERFACE_LANGUAGE_OPTIONS,
  normalizeInterfaceLanguage,
  toIntlLocale,
  type InterfaceLanguageCode,
} from './languages'

type Dictionary = { translation: Record<string, string> }
const dictionaries: Record<
  InterfaceLanguageCode,
  () => Promise<{ default: Dictionary }>
> = {
  en: () => import('./locales/en.json'),
  zh: () => import('./locales/zh.json'),
  'zh-TW': () => import('./locales/zh-TW.json'),
  fr: () => import('./locales/fr.json'),
  ru: () => import('./locales/ru.json'),
  ja: () => import('./locales/ja.json'),
  vi: () => import('./locales/vi.json'),
}
const pendingDictionaries = new Map<string, Promise<void>>()

function loadDictionary(language: InterfaceLanguageCode): Promise<void> {
  let pending = pendingDictionaries.get(language)
  if (!pending) {
    pending = dictionaries[language]()
      .then(({ default: dictionary }) => {
        i18n.addResourceBundle(language, 'translation', dictionary.translation)
      })
      .catch((error: unknown) => {
        pendingDictionaries.delete(language)
        const label = INTERFACE_LANGUAGE_OPTIONS.find(
          (option) => option.code === language
        )!.label
        throw new Error(`${i18n.t('Failed to load')}: ${label}`, {
          cause: error,
        })
      })
    pendingDictionaries.set(language, pending)
  }
  return pending
}

const initialLanguage = normalizeInterfaceLanguage(
  typeof window === 'undefined'
    ? null
    : window.localStorage.getItem('i18nextLng')
) as InterfaceLanguageCode

const initialized = i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {},
    initAsync: false,
    lng: initialLanguage,
    fallbackLng: 'en',
    supportedLngs: ['en', 'zh', 'zh-TW', 'fr', 'ru', 'ja', 'vi'],
    nsSeparator: false, // Allow literal colons in keys (e.g., URLs, labels)
    debug: import.meta.env?.DEV ?? false,
    interpolation: {
      escapeValue: false, // not needed for react as it escapes by default
    },
    detection: {
      order: ['localStorage'],
      caches: ['localStorage'],
      convertDetectedLanguage: normalizeInterfaceLanguage,
    },
  })

const changeLanguage = i18n.changeLanguage.bind(i18n)

// The app waits for both dictionaries before its first render. English remains
// available for fallback keys without loading the other five interface languages.
export const i18nReady = initialized.then(async () => {
  await Promise.all([loadDictionary(initialLanguage), loadDictionary('en')])
  await changeLanguage(initialLanguage)
})

let languageChange = 0
i18n.changeLanguage = ((language, callback) => {
  const requestedLanguage = normalizeInterfaceLanguage(
    language ?? i18n.language
  ) as InterfaceLanguageCode
  const request = ++languageChange
  return i18nReady
    .then(() => loadDictionary(requestedLanguage))
    .then(() => {
      if (request === languageChange) {
        return changeLanguage(requestedLanguage, callback)
      }
      const t = i18n.t.bind(i18n)
      callback?.(null, t)
      return t
    })
    .catch((error: unknown) => {
      callback?.(error, i18n.t.bind(i18n))
      throw error
    })
}) as typeof i18n.changeLanguage

if (typeof document !== 'undefined') {
  const updateDocumentLanguage = (language: string) => {
    document.documentElement.lang = toIntlLocale(language) ?? 'en'
  }
  i18n.on('languageChanged', updateDocumentLanguage)
  updateDocumentLanguage(i18n.language)
}

export default i18n
