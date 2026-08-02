import { useCallback, useEffect, useState } from 'react'

const RULES_VIEWED_STORAGE_KEY = 'daily-lucky-number-rules-viewed'
let fallbackRulesViewed = false
let storageWarningShown = false

function reportStorageError(message: string, error: unknown) {
  if (storageWarningShown) return
  storageWarningShown = true
  // eslint-disable-next-line no-console
  console.warn(message, error)
}

function getStorage(): Storage | null {
  if (typeof window === 'undefined') return null
  try {
    return window.localStorage
  } catch (error) {
    reportStorageError(
      'Daily Lucky Number rules could not access localStorage.',
      error
    )
    return null
  }
}

function getStorageKey(storage: Storage | null): string {
  if (!storage) return `${RULES_VIEWED_STORAGE_KEY}:anonymous`
  try {
    return `${RULES_VIEWED_STORAGE_KEY}:${storage.getItem('uid') ?? 'anonymous'}`
  } catch (error) {
    reportStorageError(
      'Daily Lucky Number rules could not read the current user id.',
      error
    )
    return `${RULES_VIEWED_STORAGE_KEY}:anonymous`
  }
}

function hasViewedRules(): boolean {
  const storage = getStorage()
  if (!storage) return fallbackRulesViewed
  try {
    return storage.getItem(getStorageKey(storage)) === '1'
  } catch (error) {
    reportStorageError(
      'Daily Lucky Number rules could not read the viewed state.',
      error
    )
    return fallbackRulesViewed
  }
}

function markRulesViewed() {
  fallbackRulesViewed = true
  const storage = getStorage()
  if (!storage) return
  try {
    storage.setItem(getStorageKey(storage), '1')
  } catch (error) {
    reportStorageError(
      'Daily Lucky Number rules could not persist the viewed state.',
      error
    )
  }
}

export function useDailyLuckyRulesDialog() {
  const [open, setOpen] = useState(() => !hasViewedRules())

  useEffect(() => {
    if (open) markRulesViewed()
  }, [open])

  const openRules = useCallback(() => {
    markRulesViewed()
    setOpen(true)
  }, [])

  return { open, onOpenChange: setOpen, openRules }
}
