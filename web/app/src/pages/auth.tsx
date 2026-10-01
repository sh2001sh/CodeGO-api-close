import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { api } from '../lib/api'
import type { Schema } from '../lib/types'
import { passkeyLogin } from '../lib/passkeys'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage, Field } from '../components/ui'
import { OAuthLinks } from '../features/oauth-links'
import { safeLocalReturn } from '../lib/auth-navigation'

export default function AuthPage(props: { register?: boolean }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const search = useSearch({ strict: false })
  const returnTo = safeLocalReturn(search.returnTo)
  const resume = async () => {
    await queryClient.invalidateQueries({ queryKey: ['session'] })
    if (returnTo) window.location.assign(returnTo)
    else await navigate({ to: '/dashboard' })
  }
  const mutation = useMutation({
    mutationFn: (body: Schema['LoginInput'] | Schema['RegisterInput']) =>
      'email' in body
        ? api.POST('/api/user/register', { body })
        : api.POST('/api/user/login', { body }),
    onSuccess: resume,
  })
  const passkey = useMutation({
    mutationFn: passkeyLogin,
    onSuccess: resume,
  })
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const fields = new FormData(event.currentTarget)
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
          }
        : body,
    )
  }
  const action = props.register ? '注册' : '登录'
  return (
    <main className="auth-layout">
      <Link className="brand" to="/">
        CodeGo <span>new-api</span>
      </Link>
      <div className="auth-panel">
        <h1>{t(action)}</h1>
        <form onSubmit={submit} className="form-stack">
          <Field name="username" label="用户名" required maxLength={32} />
          {props.register && (
            <>
              <Field name="display_name" label="显示名称" maxLength={100} />
              <Field name="email" label="邮箱" type="email" required />
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
          <ErrorMessage error={mutation.error ?? passkey.error} />
          <Button type="submit" disabled={mutation.isPending}>
            {t(mutation.isPending ? '正在提交' : action)}
          </Button>
        </form>
        {!props.register && (
          <div className="section form-stack">
            <Button variant="quiet" disabled={passkey.isPending} onClick={() => passkey.mutate()}>
              {t('通行密钥登录')}
            </Button>
            <OAuthLinks returnTo={returnTo} />
          </div>
        )}
        <Link to={props.register ? '/sign-in' : '/sign-up'} search={{ returnTo }}>
          {t(props.register ? '已有账号，登录' : '创建账号')}
        </Link>
      </div>
      <footer>new-api · QuantumNous</footer>
    </main>
  )
}
