import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearch } from '@tanstack/react-router'
import { api } from '../lib/api'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage, Field } from '../components/ui'
import { AuthShell } from '../features/public/auth-shell'

export default function PasswordRecoveryPage() {
  const { t } = useTranslation()
  const send = useMutation({
    mutationFn: (email: string) => api.GET('/api/reset_password', { params: { query: { email } } }),
  })
  return (
    <AuthShell>
      <div className="auth-panel">
        <h1>{t('找回密码')}</h1>
        <p className="muted">{t('输入绑定邮箱，重置链接有效期为 10 分钟。')}</p>
        <form
          className="form-stack"
          onSubmit={(event) => {
            event.preventDefault()
            send.mutate(String(new FormData(event.currentTarget).get('email')))
          }}
        >
          <Field name="email" label="邮箱" type="email" required />
          <ErrorMessage error={send.error} />
          <Button type="submit" disabled={send.isPending}>
            {send.isPending ? t('发送中…') : t('发送重置链接')}
          </Button>
          {send.isSuccess && (
            <p role="status">{t('如果该邮箱已绑定账号，重置链接将发送到邮箱。')}</p>
          )}
        </form>
        <div className="auth-links">
          <Link to="/sign-in">{t('返回登录')}</Link>
        </div>
      </div>
    </AuthShell>
  )
}

export function ResetPasswordPage() {
  const { t } = useTranslation()
  const search = useSearch({ strict: false })
  const client = useQueryClient()
  const email = ('email' in search ? search.email : '') ?? ''
  const token = ('token' in search ? search.token : '') ?? ''
  const reset = useMutation({
    mutationFn: (password: string) =>
      api.POST('/api/user/reset', { body: { email, token, password } }),
    onSuccess: () => client.clear(),
  })
  const valid = !!email && !!token
  return (
    <AuthShell>
      <div className="auth-panel">
        <h1>{t('重置密码')}</h1>
        {!valid ? (
          <p role="alert">{t('重置链接信息不完整，请重新发送重置链接。')}</p>
        ) : reset.isSuccess ? (
          <p role="status">{t('密码已重置，原有登录已失效。请用新密码登录。')}</p>
        ) : (
          <form
            className="form-stack"
            onSubmit={(event) => {
              event.preventDefault()
              reset.mutate(String(new FormData(event.currentTarget).get('password')))
            }}
          >
            <label className="field" htmlFor="reset-password">
              <span>{t('新密码')}</span>
              <input
                id="reset-password"
                name="password"
                type="password"
                minLength={10}
                maxLength={72}
                autoComplete="new-password"
                required
              />
            </label>
            <ErrorMessage error={reset.error} />
            <Button type="submit" disabled={reset.isPending}>
              {reset.isPending ? t('重置中…') : t('重置密码')}
            </Button>
          </form>
        )}
        <div className="auth-links">
          <Link to="/forgot-password">{t('重新发送重置链接')}</Link>
          <Link to="/sign-in">{t('返回登录')}</Link>
        </div>
      </div>
    </AuthShell>
  )
}
