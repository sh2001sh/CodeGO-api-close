import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useSearch } from '@tanstack/react-router'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { passkeyLogin } from '../lib/passkeys'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage, Field } from '../components/ui'
import { OAuthLinks } from '../features/oauth-links'
import { AuthShell } from '../features/public/auth-shell'
import { safeLocalReturn } from '../lib/auth-navigation'
import { currentPolicyVersion } from '../lib/legal-policy'

export default function AuthPage(props: { register?: boolean }) {
  const { t, locale } = useTranslation()
  const queryClient = useQueryClient()
  const search = useSearch({ strict: false })
  const returnTo = safeLocalReturn(search.returnTo)
  const [challenge, setChallenge] = useState('')
  const [email, setEmail] = useState('')
  const [agreementError, setAgreementError] = useState<Error | null>(null)
  const resume = () => {
    // A successful authentication may replace another user's session in this SPA.
    queryClient.clear()
    // A new document also discards cached route loaders and mounted query observers.
    window.location.assign(returnTo ?? '/dashboard')
  }
  const mutation = useMutation({
    mutationFn: (body: Schema['LoginInput'] | Schema['RegisterInput']) =>
      'email' in body
        ? api.POST('/api/user/register', { body })
        : api.POST('/api/user/login', { body }),
    onSuccess: async (result) => {
      const data = unwrap(result)
      if ('require_2fa' in data && data.require_2fa && 'challenge_token' in data) {
        setChallenge(String(data.challenge_token))
        return
      }
      await resume()
    },
  })
  const verify = useMutation({
    mutationFn: (code: string) =>
      api.POST('/api/user/login/2fa', { body: { code, challenge_token: challenge } }),
    onSuccess: resume,
  })
  const sendCode = useMutation({
    mutationFn: () => api.GET('/api/verification', { params: { query: { email } } }),
  })
  const passkey = useMutation({
    mutationFn: passkeyLogin,
    onSuccess: resume,
  })
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const fields = new FormData(event.currentTarget)
    if (props.register && fields.get('policy_acceptance') !== 'on') {
      setAgreementError(new Error('请阅读并同意服务条款与隐私政策。'))
      return
    }
    setAgreementError(null)
    const body = {
      username: String(fields.get('username')),
      password: String(fields.get('password')),
    }
    mutation.mutate(
      props.register
        ? {
            ...body,
            email: String(fields.get('email')),
            display_name: String(fields.get('display_name')),
            verification_code: String(fields.get('verification_code') ?? '') || undefined,
            aff_code: String(search.ref ?? '') || undefined,
            accepted_terms_version: currentPolicyVersion,
            accepted_privacy_version: currentPolicyVersion,
            agreement_locale: locale,
          }
        : body,
    )
  }
  const action = props.register ? '注册' : '登录'
  return (
    <AuthShell>
      <div className="auth-panel">
        <h1>{t(action)}</h1>
        {challenge ? (
          <form
            className="form-stack"
            onSubmit={(event) => {
              event.preventDefault()
              verify.mutate(String(new FormData(event.currentTarget).get('code')).trim())
            }}
          >
            <p className="muted">{t('输入验证器的验证码，或输入一个尚未使用的备用码。')}</p>
            <Field name="code" label="两步验证码或备用码" required maxLength={32} />
            <ErrorMessage error={verify.error} />
            <Button type="submit" disabled={verify.isPending}>
              {verify.isPending ? t('验证中…') : t('验证并登录')}
            </Button>
            <Button
              variant="quiet"
              onClick={() => {
                setChallenge('')
                verify.reset()
              }}
            >
              {t('返回密码登录')}
            </Button>
          </form>
        ) : (
          <form onSubmit={submit} className="form-stack">
            <Field name="username" label="用户名" required maxLength={32} />
            {props.register && (
              <>
                <Field name="display_name" label="显示名称" maxLength={100} />
                <label className="field" htmlFor="email">
                  <span>{t('邮箱')}</span>
                  <input
                    id="email"
                    name="email"
                    type="email"
                    required
                    value={email}
                    onChange={(event) => {
                      setEmail(event.target.value)
                      sendCode.reset()
                    }}
                  />
                </label>
                <Field name="verification_code" label="邮箱验证码" maxLength={32} />
                <Button
                  variant="quiet"
                  disabled={!email || sendCode.isPending}
                  onClick={() => sendCode.mutate()}
                >
                  {t('发送邮箱验证码')}
                </Button>
                <ErrorMessage error={sendCode.error} />
                {sendCode.isSuccess && <p role="status">{t('验证码已发送，请查看邮箱。')}</p>}
              </>
            )}
            <label className="field" htmlFor="password">
              <span>{t('密码')}</span>
              <input
                name="password"
                id="password"
                type="password"
                minLength={props.register ? 10 : 1}
                maxLength={72}
                required
                autoComplete={props.register ? 'new-password' : 'current-password'}
              />
            </label>
            {props.register && (
              <label className="checkbox-field">
                <input
                  type="checkbox"
                  name="policy_acceptance"
                  required
                  onChange={() => setAgreementError(null)}
                />
                <span>
                  {t('我已阅读并同意')}{' '}
                  <Link to="/terms" target="_blank" rel="noopener noreferrer">
                    {t('服务条款')}
                  </Link>{' '}
                  {t('与')}{' '}
                  <Link to="/privacy" target="_blank" rel="noopener noreferrer">
                    {t('隐私政策')}
                  </Link>
                  {' · '}
                  {t('版本')} {currentPolicyVersion}
                </span>
              </label>
            )}
            <ErrorMessage error={agreementError ?? mutation.error ?? passkey.error} />
            <Button type="submit" disabled={mutation.isPending}>
              {t(mutation.isPending ? '正在提交' : action)}
            </Button>
          </form>
        )}
        {!props.register && !challenge && (
          <>
            <Button variant="quiet" disabled={passkey.isPending} onClick={() => passkey.mutate()}>
              {t('通行密钥登录')}
            </Button>
            <div className="auth-divider">{t('或')}</div>
            <OAuthLinks returnTo={returnTo} />
          </>
        )}
        <div className="auth-links">
          {!props.register && !challenge && <Link to="/forgot-password">{t('找回密码')}</Link>}
          <Link
            to={props.register ? '/sign-in' : '/sign-up'}
            search={{ returnTo, ref: search.ref }}
          >
            {t(props.register ? '已有账号，登录' : '创建账号')}
          </Link>
        </div>
        <nav className="auth-policy-links" aria-label={t('公司与政策')}>
          <Link to="/terms">{t('服务条款')}</Link>
          <Link to="/privacy">{t('隐私政策')}</Link>
          <Link to="/support">{t('联系支持')}</Link>
        </nav>
      </div>
    </AuthShell>
  )
}
