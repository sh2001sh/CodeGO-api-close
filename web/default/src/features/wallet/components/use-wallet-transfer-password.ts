import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useCountdown } from '@/hooks/use-countdown'
import { useSecureVerification } from '@/features/auth/secure-verification'
import {
  configureWalletTransferPassword,
  isApiSuccess,
  sendWalletTransferPasswordEmailCode,
} from '../api'
import type {
  ConfigureWalletTransferPasswordRequest,
  WalletTransferSecurityOverview,
} from '../types'

export function useWalletTransferPassword(options: {
  security: WalletTransferSecurityOverview | undefined
  reload: () => Promise<void>
}) {
  const { security, reload } = options
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [emailSending, setEmailSending] = useState(false)
  const countdown = useCountdown({ initialSeconds: 60 })

  const finish = useCallback(async () => {
    toast.success(t('Payment password saved.'))
    setOpen(false)
    countdown.reset()
    await reload()
  }, [countdown, reload, t])
  const verification = useSecureVerification({ onSuccess: () => void finish() })

  const configure = async (request: ConfigureWalletTransferPasswordRequest) => {
    const perform = async () => {
      const response = await configureWalletTransferPassword(request)
      if (!isApiSuccess(response)) {
        throw new Error(
          response.message || t('Failed to save payment password.')
        )
      }
      return response
    }
    setSubmitting(true)
    try {
      if (!security?.password_set && !security?.requires_account_password) {
        const result = await verification.withVerification(perform, {
          title: t('Verify before setting a payment password'),
          description: t(
            'Confirm your identity with 2FA or Passkey before enabling quota transfers.'
          ),
        })
        if (result) await finish()
        return
      }
      await perform()
      await finish()
    } catch (reason) {
      toast.error(
        reason instanceof Error
          ? reason.message
          : t('Failed to save payment password.')
      )
    } finally {
      setSubmitting(false)
    }
  }

  const sendEmailCode = async () => {
    setEmailSending(true)
    try {
      const response = await sendWalletTransferPasswordEmailCode()
      if (!isApiSuccess(response)) {
        throw new Error(response.message || '验证码发送失败')
      }
      countdown.start()
      toast.success(
        `验证码已发送至 ${response.data?.email_masked || '绑定邮箱'}`
      )
    } catch (reason) {
      toast.error(reason instanceof Error ? reason.message : '验证码发送失败')
    } finally {
      setEmailSending(false)
    }
  }

  return {
    open,
    setOpen,
    submitting,
    configure,
    verification,
    emailSending,
    emailSecondsLeft: countdown.secondsLeft,
    emailCountdownActive: countdown.isActive,
    sendEmailCode,
  }
}
