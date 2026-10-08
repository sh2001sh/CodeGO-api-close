import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { positiveID, errorFrom } from './commerce/amounts'
import { Button, ErrorMessage, Field, Loading } from '../components/ui'

export function AdminTwoFactor() {
  const { t } = useTranslation()
  const stats = useQuery(
    resourceOptions('two-factor-stats', (signal) =>
      api.GET('/api/user/2fa/stats', { signal }).then(unwrap),
    ),
  )
  const [target, setTarget] = useState('')
  const [error, setError] = useState<Error | null>(null)
  const reset = useMutation({
    mutationFn: () => api.DELETE('/api/user/{id}/2fa', { params: { path: { id: target } } }),
    onSuccess: () => {
      setTarget('')
      void stats.refetch()
    },
  })
  return (
    <details className="section">
      <summary>{t('两步验证管理')}</summary>
      <ErrorMessage error={error ?? stats.error ?? reset.error} />
      {stats.isPending && <Loading />}
      {stats.data && (
        <p>
          {t('已启用')} {String(stats.data.enabled_users)} / {String(stats.data.total_users)}{' '}
          {t('位用户 ·')} {stats.data.enabled_rate}
        </p>
      )}
      <form
        className="form-panel"
        onSubmit={(event) => {
          event.preventDefault()
          setError(null)
          reset.reset()
          try {
            setTarget(positiveID(String(new FormData(event.currentTarget).get('two-factor-user'))))
          } catch (cause) {
            setError(errorFrom(cause))
          }
        }}
      >
        <Field name="two-factor-user" label="需要重置两步验证的用户编号" required />
        <Button type="submit" variant="quiet" disabled={reset.isPending}>
          {t('准备重置')}
        </Button>
      </form>
      {target && (
        <div className="form-panel">
          <p>
            {t('确认移除用户')} {target} {t('的两步验证？该用户的现有登录将失效。')}
          </p>
          <Button variant="danger" disabled={reset.isPending} onClick={() => reset.mutate()}>
            {t('确认重置两步验证')}
          </Button>
          <Button variant="quiet" disabled={reset.isPending} onClick={() => setTarget('')}>
            {t('取消')}
          </Button>
        </div>
      )}
      {reset.isSuccess && <p role="status">{t('两步验证已重置。')}</p>}
    </details>
  )
}
