import { create } from 'zustand'
import { preferredLocale, type Locale } from './locales'

export type ThemePreference = 'light' | 'dark' | 'system'

const THEME_KEY = 'codego.theme'
const LOCALE_KEY = 'codego.locale'

function read(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

function write(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // Storage can be unavailable (private mode, quota); the choice still applies for this tab.
  }
}

function initialTheme(): ThemePreference {
  const stored = read(THEME_KEY)
  return stored === 'dark' || stored === 'light' || stored === 'system' ? stored : 'light'
}

export function applyTheme(theme: ThemePreference) {
  document.documentElement.dataset.theme = theme
}

export const useTheme = create<{
  theme: ThemePreference
  setTheme: (theme: ThemePreference) => void
}>((set) => ({
  theme: typeof window === 'undefined' ? 'light' : initialTheme(),
  setTheme: (theme) => {
    write(THEME_KEY, theme)
    applyTheme(theme)
    set({ theme })
  },
}))

export function storedLocale(): Locale {
  return preferredLocale(read(LOCALE_KEY), navigator.languages ?? [navigator.language])
}

export function storeLocale(locale: Locale) {
  write(LOCALE_KEY, locale)
}
