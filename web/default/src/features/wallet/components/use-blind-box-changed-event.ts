import { useEffect, type Dispatch, type SetStateAction } from 'react'
import type { BlindBoxRecord } from '../types'
import type { PrizeDialogState } from './blind-box-dialog-data'

export function useBlindBoxChangedEvent(
  setPrizeState: Dispatch<SetStateAction<PrizeDialogState>>,
  refreshAll: () => Promise<void>
) {
  useEffect(() => {
    if (typeof window === 'undefined') return
    const handleChanged = (event: Event) => {
      const detail =
        event instanceof CustomEvent && isRecord(event.detail)
          ? event.detail
          : null
      const records = Array.isArray(detail?.records)
        ? (detail.records as BlindBoxRecord[])
        : []
      if (records.length > 0) {
        setPrizeState({
          open: true,
          records,
          openCount: Number(detail?.openCount || records.length),
        })
      }
      void refreshAll()
    }
    window.addEventListener('blind-box:changed', handleChanged)
    return () => window.removeEventListener('blind-box:changed', handleChanged)
  }, [refreshAll, setPrizeState])
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
