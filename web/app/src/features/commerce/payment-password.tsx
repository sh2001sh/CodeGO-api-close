import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { Button, ErrorMessage, Field } from '../../components/ui'
import { errorFrom } from './amounts'

export function PaymentPassword(props: { security: Schema['WalletSecurity'] }) {
  const { t } = useTranslation()
  const [error, setError] = useState<Error | null>(null)
  const [saved, setSaved] = useState(false)
  const queryClient = useQueryClient()
  const locked = Number(props.security.locked_until) * 1000 > Date.now()
  const change = useMutation({
    mutationFn: (body: Schema['WalletPasswordInput']) =>
      api.PUT('/api/wallet/transfers/payment-password', { body }),
    onSuccess: () => {
      setSaved(true)
      void queryClient.invalidateQueries({ queryKey: ['transfers'] })
    },
    onError: () => {
      void queryClient.invalidateQueries({ queryKey: ['transfers'] })
    },
  })
  const security = props.security
  return (
    <section className="section">
      <h2>{security.password_set ? t('修改支付密码') : t('设置支付密码')}</h2>
      <p className="muted">{t('支付密码需包含字母和数字，长度 8–64 个字符。')}</p>
      {!security.email_recovery_available && security.password_set && (
        <p className="muted">{t('邮箱恢复暂未开放，请使用原支付密码修改。')}</p>
      )}
      {security.password_set && !locked && (
        <p className="muted">
          {t('剩余密码尝试次数：')}
          {security.remaining_password_attempts}
        </p>
      )}
      {saved && <p role="status">{t('支付密码已保存。')}</p>}
      <form
        key={`${security.password_set}-${saved}`}
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          setError(null)
          setSaved(false)
          try {
            const form = new FormData(event.currentTarget)
            const text = (name: string) => String(form.get(name) ?? '')
            const password = text('new-payment-password')
            if (
              [...password].length < 8 ||
              [...password].length > 64 ||
              !/\p{L}/u.test(password) ||
              !/\p{N}/u.test(password) ||
              password !== password.trim() ||
              new TextEncoder().encode(password).length > 72
            )
              throw new Error('支付密码需包含字母和数字，长度 8–64 个字符且最多 72 字节')
            if (password !== text('confirm-payment-password')) throw new Error('两次支付密码不一致')
            change.mutate({
              verification_method: security.password_set ? 'payment_password' : 'account_password',
              current_password: text('account-password'),
              old_payment_password: text('old-payment-password'),
              new_payment_password: password,
              confirm_password: password,
              email_code: '',
            })
          } catch (cause) {
            setError(errorFrom(cause))
          }
        }}
      >
        {security.password_set ? (
          <Field
            name="old-payment-password"
            label="原支付密码"
            type="password"
            required
            maxLength={72}
          />
        ) : (
          security.requires_account_password && (
            <Field
              name="account-password"
              label="当前登录密码"
              type="password"
              required
              maxLength={72}
            />
          )
        )}
        <Field
          name="new-payment-password"
          label="新支付密码"
          type="password"
          required
          maxLength={64}
        />
        <Field
          name="confirm-payment-password"
          label="确认支付密码"
          type="password"
          required
          maxLength={64}
        />
        <ErrorMessage error={error ?? change.error} />
        {locked && <p role="alert">{t('支付密码已临时锁定，请解锁后再试。')}</p>}
        <Button disabled={change.isPending || locked} type="submit">
          {change.isPending ? t('保存中…') : t('保存支付密码')}
        </Button>
      </form>
    </section>
  )
}
