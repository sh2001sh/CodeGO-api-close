import { useState } from 'react'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import type { Schema } from '../lib/types'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, PageHeader, Status } from '../components/ui'
import { MarketChannelForm, channelPatch } from '../features/channelmarket/channel-form'
import { MarketForm, text } from '../features/channelmarket/form'
import { OwnerAccess, OwnerBargains } from '../features/channelmarket/owner-access'
import { MarketIncome, MarketSecurity, MarketOwnerLogs } from '../features/channelmarket/reports'
import { ModelRecovery } from '../features/channelmarket/model-recovery'

export const myChannelsOptions = () =>
  resourceOptions('market-mine', (signal) =>
    api.GET('/api/marketplace/channels/mine', { signal }).then((result) => unwrap(result)),
  )

type Action = 'verify' | 'test' | 'pause' | 'resume' | 'delete' | 'pause-verification'
export default function MarketOwnerPage() {
  const client = useQueryClient()
  const channels = useSuspenseQuery(myChannelsOptions()).data
  const [editing, setEditing] = useState<Schema['ChannelMarketChannelView'] | 'new' | null>(null)
  const [selected, setSelected] = useState<Schema['ChannelMarketChannelView'] | null>(null)
  const [inviteToken, setInviteToken] = useState('')
  const [message, setMessage] = useState('')
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ['market-mine'] })
    setMessage('操作成功')
  }
  const save = useMutation({
    mutationFn: (input: { body: Schema['ChannelMarketCreateInput']; updateService: boolean }) => {
      const body = input.body
      if (editing && editing !== 'new') {
        return api.PATCH('/api/marketplace/channels/{id}', {
          params: { path: { id: editing.id } },
          body: channelPatch(body, editing, input.updateService),
        })
      }
      return api.POST('/api/marketplace/channels', { body })
    },
    onSuccess: () => {
      setEditing(null)
      refresh()
    },
  })
  const action = useMutation({
    mutationFn: (input: { id: string; action: Action }) => {
      const params = { path: { id: input.id } }
      switch (input.action) {
        case 'verify':
          return api.POST('/api/marketplace/channels/{id}/verify', { params })
        case 'test':
          return api.POST('/api/marketplace/channels/{id}/test', { params })
        case 'pause':
          return api.POST('/api/marketplace/channels/{id}/pause', { params })
        case 'resume':
          return api.POST('/api/marketplace/channels/{id}/resume', { params })
        case 'pause-verification':
          return api.POST('/api/marketplace/channels/{id}/verification/pause', { params })
        case 'delete':
          return api.DELETE('/api/marketplace/channels/{id}', { params })
      }
    },
    onSuccess: refresh,
  })
  const invite = useMutation({
    mutationFn: (input: { id: string; expires_at?: string }) =>
      api
        .POST('/api/marketplace/groups/{id}/invite', {
          params: { path: { id: input.id } },
          body: {
            expires_at: input.expires_at ?? new Date(Date.now() + 7 * 86400_000).toISOString(),
          },
        })
        .then(unwrap),
    onSuccess: (result) => setInviteToken(result.token),
  })
  return (
    <>
      <PageHeader
        title="我的渠道"
        action={
          <Button
            onClick={() => {
              setEditing('new')
              setMessage('')
            }}
          >
            提交渠道
          </Button>
        }
      />
      <ErrorMessage error={save.error ?? action.error ?? invite.error} />
      {message && (
        <p className="notice" role="status">
          {message}
        </p>
      )}
      {editing && (
        <MarketChannelForm
          key={editing === 'new' ? 'new' : editing.id}
          channel={editing === 'new' ? undefined : editing}
          pending={save.isPending}
          onSave={(body, updateService) => save.mutate({ body, updateService })}
          onCancel={() => setEditing(null)}
        />
      )}
      <DataTable
        rows={channels}
        rowKey={(row) => row.id}
        empty="暂无渠道。提交你的上游服务，验证并审核通过后即可供用户使用。"
        columns={[
          {
            label: '渠道',
            render: (row) => (
              <>
                {row.system_display_name}
                <p className="muted">
                  {row.provider_type} · {row.visibility}
                </p>
              </>
            ),
          },
          { label: '倍率', render: (row) => String(row.multiplier), numeric: true },
          { label: '状态', render: (row) => <Status value={row.lifecycle_status} /> },
          {
            label: '验证',
            render: (row) => (
              <>
                <Status value={row.verification_status} />
                {row.last_review_reason && <p className="muted">{row.last_review_reason}</p>}
              </>
            ),
          },
          {
            label: '操作',
            render: (row) => (
              <div className="row-actions">
                <Button variant="quiet" onClick={() => setEditing(row)}>
                  编辑
                </Button>
                <Button
                  variant="quiet"
                  onClick={() => {
                    setSelected(row)
                    setInviteToken('')
                  }}
                >
                  访问管理
                </Button>
                <Button
                  variant="quiet"
                  disabled={action.isPending}
                  onClick={() => action.mutate({ id: row.id, action: 'verify' })}
                >
                  验证
                </Button>
                <Button
                  variant="quiet"
                  disabled={action.isPending}
                  onClick={() => action.mutate({ id: row.id, action: 'test' })}
                >
                  测试连接
                </Button>
                {['queued', 'running'].includes(row.verification_status) && (
                  <Button
                    variant="quiet"
                    disabled={action.isPending}
                    onClick={() => action.mutate({ id: row.id, action: 'pause-verification' })}
                  >
                    暂停验证
                  </Button>
                )}
                {row.lifecycle_status === 'active' && (
                  <Button
                    variant="quiet"
                    disabled={action.isPending}
                    onClick={() => action.mutate({ id: row.id, action: 'pause' })}
                  >
                    暂停服务
                  </Button>
                )}
                {row.lifecycle_status === 'paused' && (
                  <Button
                    variant="quiet"
                    disabled={action.isPending}
                    onClick={() => action.mutate({ id: row.id, action: 'resume' })}
                  >
                    恢复服务
                  </Button>
                )}
                <Button
                  variant="danger"
                  disabled={action.isPending}
                  onClick={() => {
                    if (window.confirm(`删除渠道「${row.system_display_name}」？`))
                      action.mutate({ id: row.id, action: 'delete' })
                  }}
                >
                  删除
                </Button>
              </div>
            ),
          },
        ]}
      />
      {selected && (
        <section className="section">
          <div className="page-header">
            <h2>{selected.system_display_name} · 访问管理</h2>
            <Button variant="quiet" onClick={() => setSelected(null)}>
              收起
            </Button>
          </div>
          <MarketForm
            pending={invite.isPending}
            submit="创建邀请"
            onSubmit={(fields) => {
              const expires = text(fields, 'invite-expires')
              invite.mutate({
                id: selected.group_id,
                expires_at: expires ? new Date(expires).toISOString() : undefined,
              })
            }}
          >
            <Field name="invite-expires" label="邀请到期时间（默认 7 天）" type="datetime-local" />
          </MarketForm>
          {inviteToken && (
            <div className="notice" role="status">
              <p>邀请令牌，仅分享给需要授权的用户：</p>
              <code>{inviteToken}</code>
            </div>
          )}
          <OwnerAccess id={selected.id} internalID={selected.internal_channel_id} />
          <ModelRecovery id={selected.id} failed={selected.verification_status === 'failed'} />
        </section>
      )}
      <OwnerBargains />
      <MarketIncome />
      <MarketOwnerLogs />
      <MarketSecurity />
    </>
  )
}
