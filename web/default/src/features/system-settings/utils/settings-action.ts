import i18next from 'i18next'
import { toast } from 'sonner'

const notifiedErrors = new WeakSet<object>()

function errorMessage(error: unknown): string {
  return error instanceof Error && error.message
    ? error.message
    : i18next.t('Failed to update setting')
}

export function notifySettingsSaveError(error: unknown) {
  toast.error(errorMessage(error))
  if (error !== null && typeof error === 'object') notifiedErrors.add(error)
}

/** Catch at the UI boundary so RHF still observes rejected submissions. */
export function handleSettingsAction<TArgs extends unknown[]>(
  action: (...args: TArgs) => unknown | Promise<unknown>
) {
  return async (...args: TArgs): Promise<void> => {
    try {
      await action(...args)
    } catch (error) {
      if (
        error !== null &&
        typeof error === 'object' &&
        notifiedErrors.delete(error)
      )
        return
      toast.error(errorMessage(error))
    }
  }
}
