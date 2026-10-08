import { useEffect, useRef } from 'react'
import { Link, useLocation, useParams } from '@tanstack/react-router'
import { useTranslation } from '../lib/i18n'
import { ErrorMessage, Loading } from '../components/ui'
import { oauthCallbackTarget } from '../lib/auth-navigation'
import { AuthShell } from '../features/public/auth-shell'

export default function OAuthCallbackPage() {
  const { t } = useTranslation()
  const { provider } = useParams({ strict: false })
  const search = useLocation({ select: (location) => location.searchStr })
  const forwarded = useRef(false)
  let target: string | undefined
  let error: Error | undefined
  try {
    target = oauthCallbackTarget(provider, search)
  } catch (cause) {
    error = cause instanceof Error ? cause : new Error('外部账号回调信息无效，请重新登录。')
  }
  useEffect(() => {
    if (target && !forwarded.current) {
      forwarded.current = true
      window.location.replace(target)
    }
  }, [target])
  return (
    <AuthShell>
      <section className="auth-panel">
        <h1>{t('外部账号登录')}</h1>
        <ErrorMessage error={error} />
        {target ? <Loading /> : <Link to="/sign-in">{t('返回登录')}</Link>}
      </section>
    </AuthShell>
  )
}
