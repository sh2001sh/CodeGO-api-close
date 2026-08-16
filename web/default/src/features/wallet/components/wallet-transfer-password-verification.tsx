import { KeyRound, Loader2, MailCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export type VerificationMethod = 'payment_password' | 'email'

export function EmailBindingRequired() {
  return (
    <div className='border-border bg-muted/35 flex items-start gap-3 rounded-md border p-3 text-xs leading-5'>
      <MailCheck className='text-primary mt-0.5 size-4 shrink-0' />
      <div>
        <p className='font-medium'>请先绑定邮箱</p>
        <p className='text-muted-foreground'>
          支付密码保护额度转账，账户必须先绑定可接收安全验证码的邮箱。
        </p>
      </div>
    </div>
  )
}

export function PaymentPasswordVerification(props: {
  method: VerificationMethod
  oldPaymentPassword: string
  emailCode: string
  emailMasked: string
  emailSending: boolean
  emailSecondsLeft: number
  emailCountdownActive: boolean
  visible: boolean
  onMethodChange: (method: VerificationMethod) => void
  onOldPaymentPasswordChange: (value: string) => void
  onEmailCodeChange: (value: string) => void
  onSendEmailCode: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className='space-y-3'>
      <div>
        <Label>{t('Verification method')}</Label>
        <div className='bg-muted mt-2 grid grid-cols-2 rounded-lg p-1'>
          <VerificationMethodButton
            active={props.method === 'payment_password'}
            icon={KeyRound}
            label={t('Payment password')}
            onClick={() => props.onMethodChange('payment_password')}
          />
          <VerificationMethodButton
            active={props.method === 'email'}
            icon={MailCheck}
            label={t('Email verification')}
            onClick={() => props.onMethodChange('email')}
          />
        </div>
        <p className='text-muted-foreground mt-2 text-xs leading-5'>
          {t(
            'Choose either the current payment password or a bound-email code to verify this change.'
          )}
        </p>
      </div>

      {props.method === 'payment_password' ? (
        <PasswordField
          id='old-payment-password'
          label={t('Current payment password')}
          value={props.oldPaymentPassword}
          visible={props.visible}
          onChange={props.onOldPaymentPasswordChange}
        />
      ) : (
        <EmailCodeField {...props} />
      )}
    </div>
  )
}

function EmailCodeField(
  props: Omit<
    Parameters<typeof PaymentPasswordVerification>[0],
    | 'method'
    | 'oldPaymentPassword'
    | 'onMethodChange'
    | 'onOldPaymentPasswordChange'
  >
) {
  const { t } = useTranslation()
  return (
    <div className='space-y-2'>
      <Label htmlFor='payment-password-email-code'>
        {t('Email verification code')}
      </Label>
      <div className='flex min-w-0 gap-2'>
        <Input
          id='payment-password-email-code'
          inputMode='numeric'
          autoComplete='one-time-code'
          maxLength={6}
          value={props.emailCode}
          placeholder={`${t('Send to')} ${props.emailMasked}`}
          onChange={(event) =>
            props.onEmailCodeChange(event.target.value.replace(/\D/g, ''))
          }
        />
        <Button
          type='button'
          variant='outline'
          className='shrink-0'
          disabled={props.emailSending || props.emailCountdownActive}
          onClick={props.onSendEmailCode}
        >
          {props.emailSending ? (
            <Loader2 className='size-4 animate-spin' />
          ) : props.emailCountdownActive ? (
            `${props.emailSecondsLeft}s`
          ) : (
            t('Send code')
          )}
        </Button>
      </div>
    </div>
  )
}

function VerificationMethodButton(props: {
  active: boolean
  icon: typeof KeyRound
  label: string
  onClick: () => void
}) {
  const Icon = props.icon
  return (
    <button
      type='button'
      aria-pressed={props.active}
      onClick={props.onClick}
      className={`focus-visible:ring-ring flex min-h-9 items-center justify-center gap-1.5 rounded-md px-2 text-xs font-medium outline-none focus-visible:ring-2 ${
        props.active
          ? 'bg-background text-foreground shadow-sm'
          : 'text-muted-foreground hover:text-foreground'
      }`}
    >
      <Icon className='size-3.5' aria-hidden='true' />
      <span>{props.label}</span>
    </button>
  )
}

export function PasswordField(props: {
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
