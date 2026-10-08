import { Dialog as BaseDialog } from '@base-ui/react/dialog'
import type { ReactNode } from 'react'
import { X } from 'lucide-react'
import { create } from 'zustand'
import { useTranslation } from '../../lib/i18n'
import { Button, IconButton } from './button'

/** Centered modal for short forms and confirmations. */
export function Dialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: ReactNode
  children?: ReactNode
  footer?: ReactNode
  size?: 'md' | 'lg'
}) {
  const { t } = useTranslation()
  return (
    <BaseDialog.Root open={props.open} onOpenChange={(open) => props.onOpenChange(open)}>
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className="overlay-backdrop" />
        <BaseDialog.Popup className="dialog-popup" data-size={props.size}>
          <BaseDialog.Title className="dialog-title">{t(props.title)}</BaseDialog.Title>
          {props.description && (
            <BaseDialog.Description className="dialog-description">
              {typeof props.description === 'string' ? t(props.description) : props.description}
            </BaseDialog.Description>
          )}
          {props.children}
          {props.footer && <div className="dialog-footer">{props.footer}</div>}
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  )
}

/** Side sheet for create/edit forms and record details; keeps the list visible behind it. */
export function Drawer(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  children: ReactNode
  footer?: ReactNode
  side?: 'right' | 'left'
}) {
  const { t } = useTranslation()
  return (
    <BaseDialog.Root open={props.open} onOpenChange={(open) => props.onOpenChange(open)}>
      <BaseDialog.Portal>
        <BaseDialog.Backdrop className="overlay-backdrop" />
        <BaseDialog.Popup className="drawer-popup" data-side={props.side ?? 'right'}>
          <div className="drawer-header">
            <div>
              <BaseDialog.Title className="dialog-title">{t(props.title)}</BaseDialog.Title>
              {props.description && (
                <BaseDialog.Description className="subtle">
                  {t(props.description)}
                </BaseDialog.Description>
              )}
            </div>
            <BaseDialog.Close render={<IconButton label={t('关闭')} />}>
              <X size={18} aria-hidden />
            </BaseDialog.Close>
          </div>
          <div className="drawer-body">{props.children}</div>
          {props.footer && <div className="drawer-footer">{props.footer}</div>}
        </BaseDialog.Popup>
      </BaseDialog.Portal>
    </BaseDialog.Root>
  )
}

type ConfirmRequest = {
  title: string
  description?: string
  confirmLabel?: string
  danger?: boolean
  resolve: (value: boolean) => void
}
const useConfirmStore = create<{ request: ConfirmRequest | null }>(() => ({ request: null }))

/**
 * Promise-based replacement for window.confirm that renders an accessible dialog.
 * Mount <ConfirmHost /> once near the app root.
 */
export function confirmAction(options: Omit<ConfirmRequest, 'resolve'>): Promise<boolean> {
  return new Promise((resolve) => {
    useConfirmStore.getState().request?.resolve(false)
    useConfirmStore.setState({ request: { ...options, resolve } })
  })
}

export function ConfirmHost() {
  const { t } = useTranslation()
  const request = useConfirmStore((state) => state.request)
  const settle = (value: boolean) => {
    request?.resolve(value)
    useConfirmStore.setState({ request: null })
  }
  return (
    <Dialog
      open={request !== null}
      onOpenChange={(open) => !open && settle(false)}
      title={request?.title ?? ''}
      description={request?.description}
      footer={
        <>
          <Button variant="secondary" onClick={() => settle(false)}>
            {t('取消')}
          </Button>
          <Button
            variant={request?.danger ? 'danger-solid' : 'primary'}
            onClick={() => settle(true)}
            autoFocus
          >
            {t(request?.confirmLabel ?? '确认')}
          </Button>
        </>
      }
    />
  )
}
