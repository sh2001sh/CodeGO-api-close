import { X } from 'lucide-react'
import { useToast } from '../hooks/use-toast'
import { useTranslation } from '../lib/i18n'

export function ToastContainer() {
  const { t } = useTranslation()
  const { toasts, remove } = useToast()
  return (
    <div className="toast-container" aria-live="polite">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className="toast"
          data-variant={toast.variant}
          role={toast.variant === 'error' ? 'alert' : 'status'}
        >
          <span className="toast-message">{toast.message}</span>
          <button className="toast-close" onClick={() => remove(toast.id)} aria-label={t('关闭')}>
            <X size={14} aria-hidden />
          </button>
        </div>
      ))}
    </div>
  )
}
