import { useTranslation } from '../lib/i18n'
import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { ErrorMessage, Loading } from '../components/ui'
import { oauthStartURL } from '../lib/auth-navigation'

export function OAuthLinks(props: { bind?: boolean; returnTo?: string }) {
  const { t } = useTranslation()
  const providers = useQuery(
    resourceOptions('oauth-providers', (signal) =>
      api.GET('/api/oauth/providers', { signal }).then(unwrap),
    ),
  )
  return (
    <>
      <ErrorMessage error={providers.error} />
      {providers.isPending && <Loading />}
      <div className="filters">
        {providers.data?.map((provider) => (
          <a
            key={provider.slug}
            className="button button-quiet"
            href={oauthStartURL(provider.slug, props)}
          >
            {provider.name}
          </a>
        ))}
      </div>
      {!providers.isPending && !providers.isError && !providers.data?.length && (
        <p className="muted">{t('暂无可用外部账号。')}</p>
      )}
    </>
  )
}
