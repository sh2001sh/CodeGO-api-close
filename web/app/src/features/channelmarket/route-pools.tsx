import { useState } from 'react'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions, keysOptions } from '../../lib/queries'
import type { Schema } from '../../lib/types'
import { Button, ErrorMessage, Field } from '../../components/ui'
import { DataTable } from '../../components/data-table'
import { MarketForm, text, integer } from './form'

export const poolOptions = () =>
  resourceOptions('market-pools', (signal) =>
    api.GET('/api/marketplace/route-pools', { signal }).then((result) => unwrap(result)),
  )

export function MarketRoutePools() {
  const client = useQueryClient()
  const pools = useSuspenseQuery(poolOptions()).data
  const groups = useSuspenseQuery(
    resourceOptions('market-groups', (signal) =>
      api.GET('/api/marketplace/key-group-options', { signal }).then((result) => unwrap(result)),
    ),
  ).data
  const keys = useSuspenseQuery(keysOptions()).data
  const [editing, setEditing] = useState<Schema['ChannelMarketRoutePool'] | 'new' | 'auto' | null>(
    null,
  )
  const [binding, setBinding] = useState('')
  const refresh = () => {
    setEditing(null)
    void client.invalidateQueries({ queryKey: ['market-pools'] })
  }
  const save = useMutation({
    mutationFn: (body: Schema['ChannelMarketRoutePool']) =>
      editing === 'auto'
        ? api.PUT('/api/marketplace/auto-route-pool', { body })
        : body.id
          ? api.PUT('/api/marketplace/route-pools/{id}', {
              params: { path: { id: body.id } },
              body,
            })
          : api.POST('/api/marketplace/route-pools', { body }),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id: string) =>
      api.DELETE('/api/marketplace/route-pools/{id}', { params: { path: { id } } }),
    onSuccess: refresh,
  })
  const bind = useMutation({
    mutationFn: (token: bigint) =>
      api.POST('/api/marketplace/route-pools/{id}/bind-token', {
        params: { path: { id: binding } },
        body: { token_id: token },
      }),
    onSuccess: () => {
      setBinding('')
      void client.invalidateQueries({ queryKey: ['keys'] })
    },
  })
  const current =
    editing === 'auto'
      ? pools.find((pool) => pool.name === 'Auto')
      : editing && typeof editing === 'object'
        ? editing
        : undefined
  return (
    <section className="section">
      <div className="page-header">
        <h2>路由池</h2>
        <div>
          <Button variant="quiet" onClick={() => setEditing('auto')}>
            配置自动路由池
          </Button>
          <Button onClick={() => setEditing('new')}>创建路由池</Button>
        </div>
      </div>
      <ErrorMessage error={save.error ?? remove.error ?? bind.error} />
      {editing && (
        <MarketForm
          key={current?.id ?? String(editing)}
          pending={save.isPending}
          onSubmit={(fields) => {
            const members = fields
              .getAll('groups')
              .map((id, index) => ({ group_id: String(id), priority: 100 - index }))
            if (!members.length) throw new Error('至少选择一个渠道组')
            save.mutate({
              id: current?.id ?? '',
              owner_user_id: current?.owner_user_id ?? 0,
              ...(current ?? {}),
              name: editing === 'auto' ? 'Auto' : text(fields, 'pool-name'),
              strategy: text(fields, 'strategy'),
              max_attempts: Number(text(fields, 'attempts')),
              failure_cooldown_seconds: Number(text(fields, 'cooldown')),
              max_multiplier: Number(text(fields, 'maximum') || '0'),
              members,
            })
          }}
        >
          {editing !== 'auto' && (
            <Field
              name="pool-name"
              label="路由池名称"
              required
              defaultValue={current?.name}
              maxLength={64}
            />
          )}
          <label className="field" htmlFor="pool-strategy">
            <span>选择策略</span>
            <select
              id="pool-strategy"
              name="strategy"
              defaultValue={current?.strategy ?? 'priority'}
            >
              <option value="priority">优先级</option>
              <option value="weighted">权重</option>
              <option value="round_robin">轮询</option>
              <option value="fill_first">顺序填满</option>
            </select>
          </label>
          <Field
            name="attempts"
            label="最大尝试次数"
            type="number"
            min={1}
            defaultValue={current?.max_attempts ?? 3}
          />
          <Field
            name="cooldown"
            label="失败冷却（秒）"
            type="number"
            min={0}
            defaultValue={current?.failure_cooldown_seconds ?? 30}
          />
          <Field
            name="maximum"
            label="最高倍率（0 不限）"
            defaultValue={String(current?.max_multiplier ?? 0)}
          />
          <fieldset style={{ flexBasis: '100%' }}>
            <legend>加入渠道组（按列表顺序排列优先级）</legend>
            {groups.map((group) => (
              <label key={group.group_id} className="filters">
                <input
                  type="checkbox"
                  name="groups"
                  value={group.group_id}
                  defaultChecked={current?.members?.some(
                    (member) => member.group_id === group.group_id,
                  )}
                />
                {group.system_display_name} · {String(group.multiplier)} 倍
              </label>
            ))}
            {!groups.length && (
              <p className="muted">暂无可选择的渠道组，先接受私有组邀请或选择公开渠道。</p>
            )}
          </fieldset>
          <Button variant="quiet" type="button" onClick={() => setEditing(null)}>
            取消
          </Button>
        </MarketForm>
      )}
      <DataTable
        rows={pools}
        rowKey={(row) => row.id}
        empty="暂无路由池。创建后可将 Key 绑定到多个渠道组。"
        columns={[
          { label: '名称', render: (row) => row.name },
          {
            label: '策略',
            render: (row) =>
              ({
                priority: '优先级',
                weighted: '权重',
                round_robin: '轮询',
                fill_first: '顺序填满',
              })[row.strategy] ?? row.strategy,
          },
          {
            label: '渠道组',
            render: (row) =>
              row.members
                ?.map(
                  (member) =>
                    groups.find((group) => group.group_id === member.group_id)
                      ?.system_display_name ?? member.group_id,
                )
                .join(', '),
          },
          {
            label: '操作',
            render: (row) => (
              <div className="row-actions">
                <Button variant="quiet" onClick={() => setEditing(row)}>
                  编辑
                </Button>
                <Button variant="quiet" onClick={() => setBinding(row.id)}>
                  绑定 Key
                </Button>
                <Button
                  variant="danger"
                  disabled={remove.isPending}
                  onClick={() => {
                    if (window.confirm(`删除路由池「${row.name}」？已绑定 Key 的路由池不能删除。`))
                      remove.mutate(row.id)
                  }}
                >
                  删除
                </Button>
              </div>
            ),
          },
        ]}
      />
      {binding && (
        <MarketForm
          pending={bind.isPending}
          submit="绑定路由池"
          onSubmit={(fields) => bind.mutate(integer(fields, 'pool-key'))}
        >
          <label className="field" htmlFor="pool-key">
            <span>API Key</span>
            <select id="pool-key" name="pool-key" required defaultValue="">
              <option value="" disabled>
                选择 Key
              </option>
              {keys
                .filter((key) => key.status === 'active')
                .map((key) => (
                  <option key={String(key.id)} value={String(key.id)}>
                    {key.name}
                  </option>
                ))}
            </select>
          </label>
          <Button variant="quiet" type="button" onClick={() => setBinding('')}>
            取消
          </Button>
        </MarketForm>
      )}
    </section>
  )
}
