import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { api, unwrap } from '../lib/api'
import type { Schema } from '../lib/types'
import { resourceOptions, sessionOptions } from '../lib/queries'
import { passkeyRegister } from '../lib/passkeys'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage, Field, PageHeader } from '../components/ui'
import { OAuthLinks } from '../features/oauth-links'

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
  return (
    <>
      <PageHeader title="个人资料" />
      <ErrorMessage error={save.error ?? register.error} />
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          const fields = new FormData(event.currentTarget)
          save.mutate({
            display_name: String(fields.get('display_name')),
            email: String(fields.get('email')),
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
        <Field name="email" label="邮箱" type="email" defaultValue={user.email} />
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
      <section className="section">
        <h2>{t('通行密钥')}</h2>
        <p className="notice">
          {t('已绑定')} · {passkey.count}
        </p>
        <Button disabled={register.isPending} onClick={() => register.mutate()}>
          {t('添加通行密钥')}
        </Button>
      </section>
      <section className="section">
        <h2>{t('外部账号绑定')}</h2>
        <OAuthLinks bind />
      </section>
    </>
  )
}
