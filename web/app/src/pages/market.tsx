import { useState } from 'react'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions, keysOptions } from '../lib/queries'
import type { Schema } from '../lib/types'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, PageHeader, Status } from '../components/ui'
import { MarketForm, text, integer, factor } from '../features/channelmarket/form'
import { MarketRoutePools } from '../features/channelmarket/route-pools'
import { MarketPrices } from '../features/channelmarket/prices'

export const marketOptions = () =>
  resourceOptions('market-groups', (signal) =>
    api.GET('/api/marketplace/key-group-options', { signal }).then((result) => unwrap(result)),
  )
export const noticeOptions = () =>
  resourceOptions('market-notices', (signal) =>
    api.GET('/api/marketplace/multiplier-notices', { signal }).then((result) => unwrap(result)),
  )

export default function MarketPage() {
  const client = useQueryClient()
  const groups = useSuspenseQuery(marketOptions()).data
  const keys = useSuspenseQuery(keysOptions()).data
  const notices = useSuspenseQuery(noticeOptions()).data
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<Schema['ChannelMarketChannelView'] | null>(null)
  const [acceptedGroup, setAcceptedGroup] = useState('')
  const [message, setMessage] = useState('')
  const refresh = () => {
    setMessage('已保存')
    void client.invalidateQueries({ queryKey: ['market-groups'] })
    void client.invalidateQueries({ queryKey: ['keys'] })
  }
  const bind = useMutation({
    mutationFn: (body: { id: string; token: bigint }) =>
      api.POST('/api/marketplace/groups/{id}/bind-token', {
        params: { path: { id: body.id } },
        body: { token_id: body.token },
      }),
    onSuccess: refresh,
  })
  const invite = useMutation({
    mutationFn: (token: string) =>
      api.POST('/api/marketplace/invites/accept', { body: { token } }).then(unwrap),
    onSuccess: (result) => {
      setAcceptedGroup(result.group_id)
      refresh()
    },
  })
  const bargain = useMutation({
    mutationFn: (body: { id: string; multiplier: number; reason: string }) =>
      api.POST('/api/marketplace/groups/{id}/bargain-requests', {
        params: { path: { id: body.id } },
        body: { proposed_multiplier: body.multiplier, reason: body.reason },
      }),
    onSuccess: () => setMessage('议价申请已提交'),
  })
  const feedback = useMutation({
    mutationFn: (id: string) =>
      api.POST('/api/marketplace/groups/{id}/feedback', { params: { path: { id } } }),
    onSuccess: () => setMessage('反馈已提交'),
  })
  const read = useMutation({
    mutationFn: (id: string) =>
      api.POST('/api/marketplace/multiplier-notices/{id}/read', { params: { path: { id } } }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['market-notices'] }),
  })
  const error = bind.error ?? invite.error ?? bargain.error ?? feedback.error ?? read.error
  return (
    <>
      <PageHeader title="渠道市场" />
      <ErrorMessage error={error} />
      {message && (
        <p role="status" className="notice">
          {message}
        </p>
      )}
      <label className="field filters" htmlFor="market-search">
        <span>搜索渠道或模型</span>
        <input
          id="market-search"
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
      </label>
      <DataTable
        rows={groups.filter((group) =>
          `${group.system_display_name} ${group.declared_models?.join(' ')}`
            .toLowerCase()
            .includes(search.toLowerCase()),
        )}
        rowKey={(group) => group.group_id}
        empty="暂无可用渠道。可接受渠道主的私有组邀请。"
        columns={[
          {
            label: '渠道',
            render: (group) => (
              <>
                <strong>{group.system_display_name}</strong>
                <p className="muted">{group.approved_source_label}</p>
              </>
            ),
          },
          { label: '模型', render: (group) => group.declared_models?.join(', ') },
          { label: '倍率', render: (group) => String(group.multiplier), numeric: true },
          { label: '验证', render: (group) => <Status value={group.verification_status} /> },
          {
            label: '操作',
            render: (group) => (
              <Button
                variant="quiet"
                onClick={() => {
                  setSelected(group)
                  setMessage('')
                }}
              >
                选择渠道
              </Button>
            ),
          },
        ]}
      />
      {selected && (
        <section className="section">
          <h2>{selected.system_display_name}</h2>
          <details>
            <summary>模型价格</summary>
            <MarketPrices prices={selected.model_prices} />
          </details>
          <MarketForm
            pending={bind.isPending}
            submit="绑定 Key"
            onSubmit={(fields) =>
              bind.mutate({ id: selected.group_id, token: integer(fields, 'market-key') })
            }
          >
            <label className="field" htmlFor="market-key">
              <span>API Key</span>
              <select id="market-key" name="market-key" required defaultValue="">
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
          </MarketForm>
          <MarketForm
            pending={bargain.isPending}
            submit="申请专属倍率"
            onSubmit={(fields) =>
              bargain.mutate({
                id: selected.group_id,
                multiplier: factor(fields),
                reason: text(fields, 'reason'),
              })
            }
          >
            <Field
              name="multiplier"
              label="期望倍率"
              required
              defaultValue={String(selected.multiplier)}
            />
            <Field name="reason" label="议价理由" required maxLength={1000} />
          </MarketForm>
          <Button
            variant="quiet"
            disabled={feedback.isPending}
            onClick={() => feedback.mutate(selected.group_id)}
          >
            提交渠道服务反馈
          </Button>
        </section>
      )}
      <section className="section">
        <h2>接受私有组邀请</h2>
        <MarketForm
          pending={invite.isPending}
          submit="接受邀请"
          onSubmit={(fields) => invite.mutate(text(fields, 'invite-token'))}
        >
          <Field name="invite-token" label="邀请令牌" required />
        </MarketForm>
        {acceptedGroup && (
          <MarketForm
            pending={bind.isPending}
            submit="绑定私有组"
            onSubmit={(fields) =>
              bind.mutate({ id: acceptedGroup, token: integer(fields, 'private-key') })
            }
          >
            <Field name="private-key" label="API Key ID" required />
            <p className="muted">已获得组 {acceptedGroup} 的访问权限。</p>
          </MarketForm>
        )}
      </section>
      <MarketRoutePools />
      <section className="section">
        <h2>倍率变更通知</h2>
        <DataTable
          rows={notices}
          rowKey={(row) => String(row.id)}
          columns={[
            { label: '渠道', render: (row) => String(row.channel_id) },
            {
              label: '原倍率',
              render: (row) => Number(row.previous_multiplier_ppm) / 1_000_000,
              numeric: true,
            },
            {
              label: '新倍率',
              render: (row) => Number(row.multiplier_ppm) / 1_000_000,
              numeric: true,
            },
            {
              label: '操作',
              render: (row) => (
                <Button
                  variant="quiet"
                  disabled={read.isPending}
                  onClick={() => read.mutate(String(row.id))}
                >
                  标为已读
                </Button>
              ),
            },
          ]}
        />
      </section>
    </>
  )
}
