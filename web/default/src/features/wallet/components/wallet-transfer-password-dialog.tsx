import { useEffect, useState } from 'react'
import { Eye, EyeOff, Loader2, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { ConfigureWalletTransferPasswordRequest } from '../types'

export function WalletTransferPasswordDialog(props: {
  open: boolean
  onOpenChange: (open: boolean) => void
  passwordSet: boolean
  requiresAccountPassword: boolean
  submitting: boolean
  onSubmit: (request: ConfigureWalletTransferPasswordRequest) => Promise<void>
}) {
  const { t } = useTranslation()
  const [currentPassword, setCurrentPassword] = useState('')
  const [oldPaymentPassword, setOldPaymentPassword] = useState('')
  const [newPaymentPassword, setNewPaymentPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    if (!props.open) return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setCurrentPassword('')
    setOldPaymentPassword('')
    setNewPaymentPassword('')
    setConfirmPassword('')
    setVisible(false)
  }, [props.open])

  const passwordsMatch =
    newPaymentPassword.length > 0 && newPaymentPassword === confirmPassword
  const hasRequiredVerification = props.passwordSet
    ? oldPaymentPassword.length > 0
    : !props.requiresAccountPassword || currentPassword.length > 0
  const canSubmit =
    !props.submitting &&
    hasRequiredVerification &&
    passwordsMatch &&
    newPaymentPassword.length >= 8 &&
    /[A-Za-z]/.test(newPaymentPassword) &&
    /\d/.test(newPaymentPassword)

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='max-h-[calc(100dvh-1.5rem)] overflow-y-auto max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <ShieldCheck className='text-primary size-5' />
            {props.passwordSet
              ? t('Change payment password')
              : t('Set payment password')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'This password is stored separately from your login password and is required for every quota transfer.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4 py-2'>
          {props.passwordSet ? (
            <PasswordField
              id='old-payment-password'
              label={t('Current payment password')}
              value={oldPaymentPassword}
              visible={visible}
              onChange={setOldPaymentPassword}
            />
          ) : props.requiresAccountPassword ? (
            <PasswordField
              id='account-password'
              label={t('Current login password')}
              value={currentPassword}
              visible={visible}
              onChange={setCurrentPassword}
            />
          ) : (
            <div className='border-border bg-muted/35 rounded-md border px-3 py-2 text-xs leading-5'>
              {t(
                'Your account uses passwordless sign-in. A 2FA or Passkey verification will be requested.'
              )}
            </div>
          )}

          <PasswordField
            id='new-payment-password'
            label={t('New payment password')}
            value={newPaymentPassword}
            visible={visible}
            onChange={setNewPaymentPassword}
          />
          <PasswordField
            id='confirm-payment-password'
            label={t('Confirm payment password')}
            value={confirmPassword}
            visible={visible}
            onChange={setConfirmPassword}
          />

          <div className='flex items-start justify-between gap-3'>
            <p className='text-muted-foreground text-xs leading-5'>
              {t(
                'Use 8-64 characters with at least one letter and one number.'
              )}
            </p>
            <Button
              type='button'
              variant='ghost'
              size='icon'
              aria-label={visible ? t('Hide password') : t('Show password')}
              title={visible ? t('Hide password') : t('Show password')}
              onClick={() => setVisible((value) => !value)}
            >
              {visible ? (
                <EyeOff className='size-4' />
              ) : (
                <Eye className='size-4' />
              )}
            </Button>
          </div>
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            disabled={props.submitting}
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={!canSubmit}
            onClick={() =>
              void props.onSubmit({
                current_password: currentPassword,
                old_payment_password: oldPaymentPassword,
                new_payment_password: newPaymentPassword,
                confirm_password: confirmPassword,
              })
            }
          >
            {props.submitting ? (
              <Loader2 className='size-4 animate-spin' />
            ) : null}
            {t('Save payment password')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function PasswordField(props: {
  id: string
  label: string
  value: string
  visible: boolean
  onChange: (value: string) => void
}) {
  return (
    <div className='space-y-2'>
      <Label htmlFor={props.id}>{props.label}</Label>
      <Input
        id={props.id}
        type={props.visible ? 'text' : 'password'}
        value={props.value}
        autoComplete='new-password'
        maxLength={64}
        onChange={(event) => props.onChange(event.target.value)}
      />
    </div>
  )
}
