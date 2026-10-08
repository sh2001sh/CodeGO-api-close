import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from '../../lib/i18n'
import { api, unwrap } from '../../lib/api'
import { resourceOptions, keysOptions } from '../../lib/queries'
import type { Schema } from '../../lib/types'
import {
  Button,
  EmptyState,
  ErrorMessage,
  Loading,
  Status,
  confirmAction,
} from '../../components/ui'
import { MarketForm, integer } from './form'
import { PoolEditor } from './pool-editor'
import {
  orderedMembers,
  poolAutoBuild,
  poolInput,
  type Pool,
  type PoolAutoBuild,
} from './pool-settings'

export const poolOptions = () => ({
  ...resourceOptions('market-pools', (signal) =>
    api.GET('/api/marketplace/route-pools', { signal }).then(unwrap),
  ),
  refetchInterval: 30_000,
  refetchIntervalInBackground: false,
})
const groupsOptions = () =>
  resourceOptions('market-pool-group-options', (signal) =>
    api.GET('/api/marketplace/route-pools/group-options', { signal }).then(unwrap),
  )
const autoPoolOptions = () =>
  resourceOptions('market-auto-pool', (signal) =>
    api.GET('/api/marketplace/auto-route-pool', { signal }).then(unwrap),
  )

function PoolBuildStatus(props: { build: PoolAutoBuild }) {
  const { t, locale } = useTranslation()
  const date = (value: string | null | undefined) =>
    value ? new Date(value).toLocaleString(locale) : t('暂无数据')
  const buildError =
    props.build.last_error === 'No eligible groups match the selected models and multiplier limit'
      ? t('没有符合模型及倍率上限的分组；保留原成员，稍后重试。')
      : props.build.last_error === 'Automatic pool update failed; retry scheduled'
        ? t('自动更新失败，已安排重试。')
        : props.build.last_error
  return (
    <div className="pool-build-status">
      <Status value={props.build.enabled ? '自动更新已启用' : '自动更新已停用'} />
      <dl>
        <div>
          <dt>{t('上次更新')}</dt>
          <dd>{date(props.build.last_build_at)}</dd>
        </div>
        <div>
          <dt>{t('下次更新')}</dt>
          <dd>{props.build.enabled ? date(props.build.next_build_at) : '—'}</dd>
        </div>
      </dl>
      {props.build.last_error && (
        <p role="alert" className="pool-build-error">
          {buildError}
        </p>
      )}
    </div>
  )
}

export function MarketRoutePools() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const poolsQuery = useQuery(poolOptions())
  const groupsQuery = useQuery(groupsOptions())
  const keysQuery = useQuery(keysOptions())
  const autoQuery = useQuery(autoPoolOptions())
  const pools = poolsQuery.data ?? []
  const groups = groupsQuery.data ?? []
  const activeKeys = (keysQuery.data ?? []).filter((key) => key.status === 'active')
  const [editing, setEditing] = useState<{ pool?: Pool; automatic: boolean } | null>(null)
  const [binding, setBinding] = useState('')
  const [message, setMessage] = useState('')
  const [configError, setConfigError] = useState<Error | null>(null)
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ['market-pools'] })
    void client.invalidateQueries({ queryKey: ['market-auto-pool'] })
  }
  const save = useMutation({
    mutationFn: (input: { automatic: boolean; body: Schema['ChannelMarketRoutePoolInput'] }) =>
      input.automatic
        ? api.PUT('/api/marketplace/auto-route-pool', { body: input.body })
        : input.body.id
          ? api.PUT('/api/marketplace/route-pools/{id}', {
              params: { path: { id: input.body.id } },
              body: input.body,
            })
          : api.POST('/api/marketplace/route-pools', { body: input.body }),
    onSuccess: () => {
      setEditing(null)
      setMessage('路由池已保存')
      refresh()
    },
    onMutate: () => setMessage(''),
  })
  const remove = useMutation({
    mutationFn: (id: string) =>
      api.DELETE('/api/marketplace/route-pools/{id}', { params: { path: { id } } }),
    onSuccess: () => {
      setMessage('路由池已删除')
      refresh()
    },
  })
  const run = useMutation({
    mutationFn: (id: string) =>
      api.POST('/api/marketplace/route-pools/{id}/auto-build/run', { params: { path: { id } } }),
    onMutate: () => setMessage(''),
    onSuccess: () => {
      setMessage('路由池已更新')
    },
    onSettled: refresh,
  })
  const bind = useMutation({
    mutationFn: (token: bigint) =>
      api.POST('/api/marketplace/route-pools/{id}/bind-token', {
        params: { path: { id: binding } },
        body: { token_id: token },
      }),
    onSuccess: () => {
      setBinding('')
      setMessage('路由池已绑定')
      void client.invalidateQueries({ queryKey: ['keys'] })
      void client.invalidateQueries({ queryKey: ['conversation-groups'] })
    },
  })
  const openEditor = (pool: Pool | undefined, automatic: boolean) => {
    try {
      poolAutoBuild(pool)
      setConfigError(null)
      setMessage('')
      setEditing({ pool, automatic })
    } catch (error) {
      setConfigError(error instanceof Error ? error : new Error('自动更新配置无效'))
    }
  }
  return (
    <section className="section market-pools">
      <div className="page-header">
        <div>
          <h2>{t('路由池')}</h2>
          <p className="subtle">{t('手动安排分组顺序，或让服务器按计划更新成员。')}</p>
        </div>
        <div className="row-actions">
          <Button
            variant="secondary"
            disabled={
              autoQuery.isPending ||
              Boolean(autoQuery.error) ||
              groupsQuery.isPending ||
              Boolean(groupsQuery.error) ||
              save.isPending
            }
            onClick={() => openEditor(autoQuery.data, true)}
          >
            {t('配置自动路由池')}
          </Button>
          <Button
            disabled={save.isPending || groupsQuery.isPending || Boolean(groupsQuery.error)}
            onClick={() => openEditor(undefined, false)}
          >
            {t('创建路由池')}
          </Button>
        </div>
      </div>
      <ErrorMessage
        error={
          poolsQuery.error ??
          groupsQuery.error ??
          keysQuery.error ??
          autoQuery.error ??
          configError ??
          save.error ??
          remove.error ??
          run.error ??
          bind.error
        }
      />
      {(poolsQuery.error || groupsQuery.error || autoQuery.error) && (
        <Button
          variant="secondary"
          onClick={() => {
            void poolsQuery.refetch()
            void groupsQuery.refetch()
            void autoQuery.refetch()
          }}
        >
          {t('重试')}
        </Button>
      )}
      {message && (
        <p role="status" className="notice">
          {t(message)}
        </p>
      )}
      {editing && (
        <PoolEditor
          key={`${editing.pool?.id ?? 'new'}-${editing.automatic}`}
          pool={editing.pool}
          automatic={editing.automatic}
          groups={groups}
          pending={save.isPending}
          onSave={(body) => save.mutate({ automatic: editing.automatic, body })}
          onCancel={() => setEditing(null)}
        />
      )}
      {poolsQuery.isPending && <Loading />}
      <div className="pool-list">
        {pools.map((pool) => {
          let build: PoolAutoBuild | undefined
          let error: Error | undefined
          try {
            build = poolAutoBuild(pool)
          } catch (failure) {
            error = failure instanceof Error ? failure : new Error('自动更新配置无效')
          }
          return (
            <article key={pool.id} className="pool-card">
              <header>
                <h3>{pool.name}</h3>
                <span className="subtle">
                  {t(
                    {
                      priority: '优先级',
                      weighted: '权重',
                      round_robin: '轮询',
                      fill_first: '顺序填满',
                      score: '评分优先',
                      cost: '低倍率优先',
                    }[pool.strategy] ?? pool.strategy,
                  )}
                </span>
              </header>
              <ol className="pool-saved-members">
                {[...(pool.members ?? [])]
                  .sort((a, b) => a.priority - b.priority)
                  .map((member) => {
                    const group = groups.find((item) => item.group_id === member.group_id)
                    return (
                      <li key={member.group_id}>
                        {group
                          ? `${group.kind === 'official' ? `${t('官方分组')} · ` : ''}${group.name} · ${group.display_id}`
                          : member.group_id.startsWith('official:')
                            ? `${t('官方分组')} · ${member.group_id.slice('official:'.length)}`
                            : t('未列出的分组')}
                      </li>
                    )
                  })}
              </ol>
              <dl className="pool-limits">
                <div>
                  <dt>{t('最大尝试次数')}</dt>
                  <dd>{pool.max_attempts}</dd>
                </div>
                <div>
                  <dt>{t('失败冷却（秒）')}</dt>
                  <dd>{pool.failure_cooldown_seconds}</dd>
                </div>
                <div>
                  <dt>{t('最高倍率（0 不限）')}</dt>
                  <dd>{pool.max_multiplier}</dd>
                </div>
              </dl>
              <ErrorMessage error={error} />
              {build && <PoolBuildStatus build={build} />}
              <div className="row-actions">
                <Button
                  variant="quiet"
                  disabled={
                    save.isPending ||
                    Boolean(error) ||
                    groupsQuery.isPending ||
                    Boolean(groupsQuery.error)
                  }
                  onClick={() => openEditor(pool, pool.name === 'Auto')}
                >
                  {t('编辑')}
                </Button>
                <Button
                  variant="quiet"
                  disabled={bind.isPending}
                  onClick={() => {
                    setBinding(pool.id)
                    setMessage('')
                  }}
                >
                  {t('绑定 Key')}
                </Button>
                <Button
                  variant="secondary"
                  disabled={run.isPending || !build?.enabled}
                  loading={run.isPending && run.variables === pool.id}
                  onClick={() => run.mutate(pool.id)}
                >
                  {t('立即更新')}
                </Button>
                <Button
                  variant="danger"
                  disabled={remove.isPending}
                  onClick={() =>
                    void confirmAction({
                      title: t('删除路由池'),
                      description: t('已绑定 Key 的路由池不能删除。'),
                      danger: true,
                    }).then((ok) => {
                      if (ok) remove.mutate(pool.id)
                    })
                  }
                >
                  {t('删除')}
                </Button>
              </div>
            </article>
          )
        })}
      </div>
      {!poolsQuery.isPending && !poolsQuery.error && !pools.length && (
        <EmptyState title="暂无路由池。创建后可将 Key 绑定到多个渠道组。" />
      )}
      {binding &&
        (activeKeys.length ? (
          <MarketForm
            pending={bind.isPending}
            submit="绑定路由池"
            onSubmit={(fields) => bind.mutate(integer(fields, 'pool-key'))}
          >
            <label className="field" htmlFor="pool-key">
              <span>API Key</span>
              <select id="pool-key" name="pool-key" required defaultValue="">
                <option value="" disabled>
                  {t('选择 Key')}
                </option>
                {activeKeys.map((key) => (
                  <option key={String(key.id)} value={String(key.id)}>
                    {key.name}
                  </option>
                ))}
              </select>
            </label>
            <Button
              variant="quiet"
              type="button"
              disabled={bind.isPending}
              onClick={() => setBinding('')}
            >
              {t('取消')}
            </Button>
          </MarketForm>
        ) : (
          <EmptyState
            title="暂无可绑定的 API Key"
            action={
              <Link to="/keys" className="button button-secondary">
                {t('创建 API Key')}
              </Link>
            }
          />
        ))}
    </section>
  )
}

export function AddToRoutePool(props: {
  group: Schema['ChannelMarketChannelView']
  onOpenPools: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const pools = useQuery(poolOptions())
  const [selected, setSelected] = useState('')
  const [message, setMessage] = useState('')
  const save = useMutation({
    mutationFn: async (id: string) => {
      const pool = pools.data?.find((item) => item.id === id)
      if (!pool) throw new Error('请选择路由池')
      const members = [...(pool.members ?? [])]
        .sort((a, b) => a.priority - b.priority)
        .map((member) => member.group_id)
      if (members.includes(props.group.group_id)) throw new Error('此分组已在路由池中')
      const body = poolInput(pool, {
        name: pool.name,
        strategy: pool.strategy,
        max_attempts: pool.max_attempts,
        failure_cooldown_seconds: pool.failure_cooldown_seconds,
        max_multiplier: pool.max_multiplier,
        members: orderedMembers([...members, props.group.group_id]),
        auto_build: poolAutoBuild(pool),
      })
      return api.PUT('/api/marketplace/route-pools/{id}', { params: { path: { id } }, body })
    },
    onSuccess: () => {
      setMessage('已加入路由池')
      void client.invalidateQueries({ queryKey: ['market-pools'] })
    },
    onMutate: () => setMessage(''),
  })
  return (
    <section className="pool-add">
      <h3>{t('加入路由池')}</h3>
      <ErrorMessage error={pools.error ?? save.error} />
      {message && <p role="status">{t(message)}</p>}
      {pools.isPending ? (
        <Loading />
      ) : pools.data?.length ? (
        <div className="row-actions">
          <label className="field" htmlFor="market-add-pool">
            <span>{t('选择路由池')}</span>
            <select
              id="market-add-pool"
              value={selected}
              onChange={(event) => setSelected(event.target.value)}
            >
              <option value="">{t('选择路由池')}</option>
              {pools.data.map((pool) => (
                <option
                  key={pool.id}
                  value={pool.id}
                  disabled={pool.members?.some(
                    (member) => member.group_id === props.group.group_id,
                  )}
                >
                  {pool.name}
                </option>
              ))}
            </select>
          </label>
          <Button
            disabled={!selected || save.isPending}
            loading={save.isPending}
            onClick={() => save.mutate(selected)}
          >
            {t('加入路由池')}
          </Button>
        </div>
      ) : (
        !pools.error && (
          <Button variant="secondary" onClick={props.onOpenPools}>
            {t('创建路由池')}
          </Button>
        )
      )}
      <p className="subtle">
        {t('追加到现有顺序末尾；启用自动更新的路由池会在下次更新时重新选择成员。')}
      </p>
    </section>
  )
}
