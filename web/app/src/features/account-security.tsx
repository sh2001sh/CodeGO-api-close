import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { Button, ErrorMessage, Field, Loading } from '../components/ui'

export function AccountSecurity() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const navigate = useNavigate()
  const status = useQuery(
    resourceOptions('two-factor', (signal) =>
      api.GET('/api/user/2fa/status', { signal }).then(unwrap),
    ),
  )
  const [action, setAction] = useState<'enable' | 'disable' | 'backup' | null>(null)
  const setup = useMutation({
    mutationFn: () => api.POST('/api/user/2fa/setup').then(unwrap),
    onSuccess: () => setAction('enable'),
  })
  const [codes, setCodes] = useState<string[]>([])
  const proof = useMutation({
    mutationFn: async (code: string) => {
      if (action === 'backup')
        return unwrap(await api.POST('/api/user/2fa/backup_codes', { body: { code } })).backup_codes
      if (action === 'disable') await api.POST('/api/user/2fa/disable', { body: { code } })
      else await api.POST('/api/user/2fa/enable', { body: { code } })
      return null
    },
    onSuccess: async (result) => {
      if (result) setCodes(result)
      else {
        client.clear()
        await navigate({ to: '/sign-in' })
        return
      }
      setAction(null)
      setup.reset()
      void client.invalidateQueries({ queryKey: ['two-factor'] })
    },
  })
  return (
    <section className="section">
      <h2>{t('两步验证')}</h2>
      <ErrorMessage error={status.error ?? setup.error ?? proof.error} />
      {status.isPending && <Loading />}
      {status.data && (
        <>
          <p>
            {status.data.enabled ? t('已启用') : t('未启用')} {t('· 剩余备用码')}{' '}
            {status.data.backup_codes_remaining}
            {status.data.locked && t(' · 暂时锁定，请稍后重试')}
          </p>
          {!action &&
            (status.data.enabled ? (
              <div className="row-actions">
                <Button
                  variant="quiet"
                  onClick={() => {
                    proof.reset()
                    setAction('backup')
                    setCodes([])
                  }}
                >
                  {t('重新生成备用码')}
                </Button>
                <Button
                  variant="danger"
                  onClick={() => {
                    proof.reset()
                    setAction('disable')
                  }}
                >
                  {t('停用两步验证')}
                </Button>
              </div>
            ) : (
              <Button
                disabled={setup.isPending}
                onClick={() => {
                  setCodes([])
                  setup.mutate()
                }}
              >
                {t('设置两步验证')}
              </Button>
            ))}
        </>
      )}
      {action === 'enable' && setup.data && (
        <div className="form-stack">
          <p>{t('在验证器中添加下面的密钥，再输入验证码启用。')}</p>
          <code className="secret">{setup.data.secret}</code>
          <p>{t('备用码仅在这里显示，请保存到安全位置；每个备用码只能使用一次。')}</p>
          <pre>{setup.data.backup_codes.join('\n')}</pre>
        </div>
      )}
      {action && (
        <form
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            proof.mutate(String(new FormData(event.currentTarget).get('two-factor-code')).trim())
          }}
        >
          {action !== 'backup' && <p>{t('更改后所有登录将失效，你需要重新登录。')}</p>}
          <Field name="two-factor-code" label="两步验证码或备用码" required maxLength={32} />
          <Button type="submit" disabled={proof.isPending}>
            {proof.isPending
              ? t('验证中…')
              : action === 'enable'
                ? t('启用两步验证')
                : action === 'backup'
                  ? t('确认生成新备用码')
                  : t('确认停用')}
          </Button>
          <Button
            variant="quiet"
            disabled={proof.isPending}
            onClick={() => {
              setAction(null)
              setup.reset()
            }}
          >
            {t('取消')}
          </Button>
        </form>
      )}
      {codes.length > 0 && (
        <div role="status">
          <p>{t('旧备用码已失效，请保存新的备用码。')}</p>
          <pre>{codes.join('\n')}</pre>
          <Button variant="quiet" onClick={() => setCodes([])}>
            {t('已保存，隐藏备用码')}
          </Button>
        </div>
      )}
    </section>
  )
}

export function EmailVerification(props: { email: string }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [email, setEmail] = useState(props.email)
  const send = useMutation({
    mutationFn: () => api.GET('/api/verification', { params: { query: { email } } }),
  })
  const verify = useMutation({
    mutationFn: (code: string) => api.POST('/api/user/email/verify', { body: { email, code } }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['session'] }),
  })
  return (
    <section className="section">
      <h2>{t('邮箱验证与绑定')}</h2>
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          verify.mutate(String(new FormData(event.currentTarget).get('binding-code')).trim())
        }}
      >
        <label className="field" htmlFor="binding-email">
          <span>{t('需要绑定或验证的邮箱')}</span>
          <input
            id="binding-email"
            type="email"
            required
            value={email}
            onChange={(event) => {
              setEmail(event.target.value)
              send.reset()
              verify.reset()
            }}
          />
        </label>
        <Field name="binding-code" label="邮箱验证码" required maxLength={32} />
        <Button
          type="button"
          variant="quiet"
          disabled={!email || send.isPending}
          onClick={() => send.mutate()}
        >
          {t('发送验证码')}
        </Button>
        <Button type="submit" disabled={verify.isPending}>
          {t('验证并绑定')}
        </Button>
        <ErrorMessage error={send.error ?? verify.error} />
        {send.isSuccess && !verify.isSuccess && <p role="status">{t('验证码已发送。')}</p>}
        {verify.isSuccess && <p role="status">{t('邮箱已验证并绑定。')}</p>}
      </form>
    </section>
  )
}
