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
import type { ConfigureWalletTransferPasswordRequest } from '../types'
import {
  PasswordField,
  PaymentPasswordVerification,
  type VerificationMethod,
} from './wallet-transfer-password-verification'

interface WalletTransferPasswordDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  passwordSet: boolean
  requiresAccountPassword: boolean
  emailBound: boolean
  emailMasked: string
  emailSending: boolean
  emailSecondsLeft: number
  emailCountdownActive: boolean
  submitting: boolean
  onSendEmailCode: () => void
  onBindEmail: () => void
  onSubmit: (request: ConfigureWalletTransferPasswordRequest) => Promise<void>
}

interface PasswordDialogFormState {
  currentPassword: string
  setCurrentPassword: (value: string) => void
  oldPaymentPassword: string
  setOldPaymentPassword: (value: string) => void
  newPaymentPassword: string
  setNewPaymentPassword: (value: string) => void
  confirmPassword: string
  setConfirmPassword: (value: string) => void
  emailCode: string
  setEmailCode: (value: string) => void
  visible: boolean
  setVisible: (value: boolean | ((current: boolean) => boolean)) => void
  verificationMethod: VerificationMethod
  setVerificationMethod: (method: VerificationMethod) => void
  canSubmit: boolean
}

export function WalletTransferPasswordDialog(
  props: WalletTransferPasswordDialogProps
) {
  const { t } = useTranslation()
  const form = usePasswordDialogForm(props)
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
        <PasswordDialogBody {...props} {...form} />
        <PasswordDialogActions {...props} {...form} />
      </DialogContent>
    </Dialog>
  )
}

function usePasswordDialogForm(
  props: Pick<
    WalletTransferPasswordDialogProps,
    'open' | 'passwordSet' | 'requiresAccountPassword' | 'submitting'
  >
): PasswordDialogFormState {
  const [currentPassword, setCurrentPassword] = useState('')
  const [oldPaymentPassword, setOldPaymentPassword] = useState('')
  const [newPaymentPassword, setNewPaymentPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [emailCode, setEmailCode] = useState('')
  const [visible, setVisible] = useState(false)
  const [verificationMethod, setVerificationMethod] =
    useState<VerificationMethod>('payment_password')

  useEffect(() => {
    if (!props.open) return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setCurrentPassword('')
    setOldPaymentPassword('')
    setNewPaymentPassword('')
    setConfirmPassword('')
    setEmailCode('')
    setVisible(false)
    setVerificationMethod('payment_password')
  }, [props.open])

  const verified = props.passwordSet
    ? verificationMethod === 'payment_password'
      ? oldPaymentPassword.length > 0
      : emailCode.length === 6
    : !props.requiresAccountPassword || currentPassword.length > 0
  const validNewPassword =
    newPaymentPassword.length >= 8 &&
    /[A-Za-z]/.test(newPaymentPassword) &&
    /\d/.test(newPaymentPassword) &&
    newPaymentPassword === confirmPassword

  return {
    currentPassword,
    setCurrentPassword,
    oldPaymentPassword,
    setOldPaymentPassword,
    newPaymentPassword,
    setNewPaymentPassword,
    confirmPassword,
    setConfirmPassword,
    emailCode,
    setEmailCode,
    visible,
    setVisible,
    verificationMethod,
    setVerificationMethod,
    canSubmit: !props.submitting && verified && validNewPassword,
  }
}

function PasswordDialogBody(
  props: WalletTransferPasswordDialogProps & PasswordDialogFormState
) {
  const { t } = useTranslation()
  return (
    <div className='space-y-4 py-2'>
      <PasswordIdentityVerification {...props} />
      <PasswordField
        id='new-payment-password'
        label={t('New payment password')}
        value={props.newPaymentPassword}
        visible={props.visible}
        onChange={props.setNewPaymentPassword}
      />
      <PasswordField
        id='confirm-payment-password'
        label={t('Confirm payment password')}
        value={props.confirmPassword}
        visible={props.visible}
        onChange={props.setConfirmPassword}
      />
      <PasswordVisibilityHint {...props} />
    </div>
  )
}

function PasswordIdentityVerification(
  props: WalletTransferPasswordDialogProps & PasswordDialogFormState
) {
  const { t } = useTranslation()
  if (props.passwordSet) {
    return (
      <PaymentPasswordVerification
        method={props.verificationMethod}
        oldPaymentPassword={props.oldPaymentPassword}
        emailCode={props.emailCode}
        emailMasked={props.emailMasked}
        emailSending={props.emailSending}
        emailSecondsLeft={props.emailSecondsLeft}
        emailCountdownActive={props.emailCountdownActive}
        emailBound={props.emailBound}
        visible={props.visible}
        onMethodChange={props.setVerificationMethod}
        onOldPaymentPasswordChange={props.setOldPaymentPassword}
        onEmailCodeChange={props.setEmailCode}
        onSendEmailCode={props.onSendEmailCode}
      />
    )
  }
  if (props.requiresAccountPassword) {
    return (
      <PasswordField
        id='account-password'
        label={t('Current login password')}
        value={props.currentPassword}
        visible={props.visible}
        onChange={props.setCurrentPassword}
      />
    )
  }
  return (
    <div className='border-border bg-muted/35 rounded-md border px-3 py-2 text-xs leading-5'>
      {t(
        'Your account uses passwordless sign-in. You can set a payment password directly.'
      )}
    </div>
  )
}

function PasswordVisibilityHint(props: PasswordDialogFormState) {
  const { t } = useTranslation()
  return (
    <div className='flex items-start justify-between gap-3'>
      <p className='text-muted-foreground text-xs leading-5'>
        {t('Use 8-64 characters with at least one letter and one number.')}
      </p>
      <Button
        type='button'
        variant='ghost'
        size='icon'
        aria-label={props.visible ? t('Hide password') : t('Show password')}
        title={props.visible ? t('Hide password') : t('Show password')}
        onClick={() => props.setVisible((value) => !value)}
      >
        {props.visible ? (
          <EyeOff className='size-4' />
        ) : (
          <Eye className='size-4' />
        )}
      </Button>
    </div>
  )
}

function PasswordDialogActions(
  props: WalletTransferPasswordDialogProps & PasswordDialogFormState
) {
  const { t } = useTranslation()
  return (
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
        disabled={!props.canSubmit}
        onClick={() => void props.onSubmit(buildPasswordRequest(props))}
      >
        {props.submitting ? <Loader2 className='size-4 animate-spin' /> : null}
        {t('Save payment password')}
      </Button>
    </DialogFooter>
  )
}

function buildPasswordRequest(
  props: WalletTransferPasswordDialogProps & PasswordDialogFormState
): ConfigureWalletTransferPasswordRequest {
  return {
    verification_method: props.passwordSet
      ? props.verificationMethod
      : undefined,
    current_password: props.currentPassword,
    old_payment_password:
      props.verificationMethod === 'payment_password'
        ? props.oldPaymentPassword
        : undefined,
    new_payment_password: props.newPaymentPassword,
    confirm_password: props.confirmPassword,
    email_code:
      props.verificationMethod === 'email' ? props.emailCode : undefined,
  }
}
