import { useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { useTranslation } from '../../lib/i18n'
import { ErrorMessage } from '../../components/ui'

export function KeyRoutingField(props: { defaultValue?: string | null }) {
  const { t } = useTranslation()
  const groups = useQuery({
    queryKey: ['conversation-groups'],
    queryFn: ({ signal }) => api.GET('/api/user/self/groups', { signal }).then(unwrap),
    staleTime: 60_000,
  })
  const market = useQuery({
    queryKey: ['market-pool-group-options'],
    queryFn: ({ signal }) =>
      api.GET('/api/marketplace/route-pools/group-options', { signal }).then(unwrap),
    staleTime: 60_000,
  })
  const pools = useQuery({
    queryKey: ['market-pools'],
    queryFn: ({ signal }) => api.GET('/api/marketplace/route-pools', { signal }).then(unwrap),
    staleTime: 60_000,
  })
  const allowed = new Set(groups.data ?? [])
  const poolGroups = new Map(
    (pools.data ?? [])
      .filter((pool) => pool.token_group)
      .map((pool) => [pool.token_group!, pool.name]),
  )
  const names = new Map(
    (market.data ?? []).map((group) => [
      group.routing_group,
      group.kind === 'market' ? `${group.name} · ${group.display_id}` : group.name,
    ]),
  )
  const regularGroups = [...allowed].filter((group) => !poolGroups.has(group))
  const routePools = [...poolGroups].filter(([group]) => allowed.has(group))
  const current = props.defaultValue
  return (
    <>
      <div className="field">
        <label className="field-label" htmlFor="key-group">
          {t('分组 / 路由池')}
        </label>
        <select id="key-group" name="group" defaultValue={current ?? ''}>
          <option value="">{t('跟随账户分组')}</option>
          {current && !allowed.has(current) && (
            <option value={current}>
              {names.get(current) ?? poolGroups.get(current) ?? current}
            </option>
          )}
          <optgroup label={t('分组')}>
            {regularGroups.map((group) => (
              <option key={group} value={group}>
                {names.get(group) ?? group}
              </option>
            ))}
          </optgroup>
          <optgroup label={t('路由池')}>
            {routePools.map(([group, name]) => (
              <option key={group} value={group}>
                {name}
              </option>
            ))}
          </optgroup>
        </select>
      </div>
      <ErrorMessage error={groups.error ?? market.error ?? pools.error} />
    </>
  )
}
