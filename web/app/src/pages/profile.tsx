import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions, sessionOptions } from '../lib/queries'
import { passkeyRegister, passkeyRemove } from '../lib/passkeys'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage, Field, PageHeader, confirmAction } from '../components/ui'
import { OAuthLinks } from '../features/oauth-links'
import { AccountSecurity, EmailVerification } from '../features/account-security'

export default function ProfilePage() {
  const { t } = useTranslation()
  const user = useSuspenseQuery(sessionOptions()).data
  const passkey = useSuspenseQuery(
    resourceOptions('passkeys', (signal) =>
      api.GET('/api/passkey', { signal }).then((result) => unwrap(result)),
    ),
  ).data
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const save = useMutation({
    mutationFn: (body: Schema['ProfileInput']) => api.PUT('/api/user/self', { body }),
    onSuccess: async (_data, body) => {
      if (body.password) {
        queryClient.clear()
        await navigate({ to: '/sign-in' })
      } else await queryClient.invalidateQueries({ queryKey: ['session'] })
    },
  })
  const register = useMutation({
    mutationFn: passkeyRegister,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['passkeys'] }),
  })
  const remove = useMutation({
    mutationFn: passkeyRemove,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['passkeys'] }),
  })
  return (
    <div className="profile-content">
      <PageHeader title="个人资料" />
      <ErrorMessage error={save.error ?? register.error ?? remove.error} />
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          const fields = new FormData(event.currentTarget)
          save.mutate({
            display_name: String(fields.get('display_name')),
            email: user.email,
            original_password: String(fields.get('original_password')),
            password: String(fields.get('password')),
          })
        }}
      >
        <Field
          name="display_name"
          label="显示名称"
          defaultValue={user.display_name}
          maxLength={100}
        />
        <Field name="original_password" label="当前密码" type="password" maxLength={72} />
        <label className="field" htmlFor="new-password">
          <span>{t('新密码')}</span>
          <input
            id="new-password"
            name="password"
            type="password"
            minLength={10}
            maxLength={72}
            autoComplete="new-password"
          />
        </label>
        <Button type="submit" disabled={save.isPending}>
          {t('保存')}
        </Button>
      </form>
      <EmailVerification email={user.email} />
      <AccountSecurity />
      <section className="section">
        <h2>{t('通行密钥')}</h2>
        <p className="notice">
          {t('已绑定')} · {passkey.count}
        </p>
        <Button disabled={register.isPending || remove.isPending} onClick={() => register.mutate()}>
          {t('添加通行密钥')}
        </Button>
        {passkey.count > 0 && (
          <Button
            variant="danger"
            disabled={register.isPending || remove.isPending}
            onClick={async () => {
              const ok = await confirmAction({
                title: '移除通行密钥',
                description:
                  '将移除此账号的全部通行密钥。需要验证当前通行密钥，并保留密码或外部账号作为其他登录方式。',
                confirmLabel: '确认移除',
                danger: true,
              })
              if (ok) remove.mutate()
            }}
          >
            {t(remove.isPending ? '验证并移除中…' : '移除通行密钥')}
          </Button>
        )}
      </section>
      <section className="section">
        <h2>{t('外部账号绑定')}</h2>
        <OAuthLinks bind />
      </section>
    </div>
  )
}
