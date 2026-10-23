import { useCallback, useState } from 'react'
import interfaceI18n from '@/i18n/config'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useCountdown } from '@/hooks/use-countdown'
import {
  configureWalletTransferPassword,
  isApiSuccess,
  sendWalletTransferPasswordEmailCode,
} from '../api'
import type { ConfigureWalletTransferPasswordRequest } from '../types'

export function useWalletTransferPassword(options: {
  reload: () => Promise<void>
}) {
  const { reload } = options
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
        throw new Error(response.message || interfaceI18n.t('验证码发送失败'))
      }
      countdown.start()
      toast.success(
        interfaceI18n.t('验证码已发送至 {{param0}}', {
          param0: response.data?.email_masked || interfaceI18n.t('绑定邮箱'),
        })
      )
    } catch (reason) {
      toast.error(
        reason instanceof Error
          ? reason.message
          : interfaceI18n.t('验证码发送失败')
      )
    } finally {
      setEmailSending(false)
    }
  }

  return {
    open,
    setOpen,
    submitting,
    configure,
    emailSending,
    emailSecondsLeft: countdown.secondsLeft,
    emailCountdownActive: countdown.isActive,
    sendEmailCode,
  }
}
