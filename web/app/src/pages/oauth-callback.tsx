import { useEffect, useRef } from 'react'
import { Link, useLocation, useParams } from '@tanstack/react-router'
import { ErrorMessage, Loading } from '../components/ui'
import { oauthCallbackTarget } from '../lib/auth-navigation'

export default function OAuthCallbackPage() {
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
    <main className="auth-layout">
      <section className="auth-panel">
        <h1>外部账号登录</h1>
        <ErrorMessage error={error} />
        {target ? <Loading /> : <Link to="/sign-in">返回登录</Link>}
      </section>
    </main>
  )
}
