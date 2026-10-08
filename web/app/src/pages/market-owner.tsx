import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from '../lib/i18n'
import '../styles/market-workspace.css'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import type { Schema } from '../lib/types'
import { DataTable } from '../components/data-table'
import {
  Button,
  CopyField,
  ErrorMessage,
  Field,
  PageHeader,
  Status,
  Tabs,
  confirmAction,
} from '../components/ui'
import { MarketChannelForm, channelPatch } from '../features/channelmarket/channel-form'
import { MarketForm, text } from '../features/channelmarket/form'
import { OwnerAccess, OwnerBargains } from '../features/channelmarket/owner-access'
import { MarketIncome, MarketSecurity } from '../features/channelmarket/reports'
import { OwnerAnalytics } from '../features/channelmarket/owner-analytics'
import { ModelRecovery } from '../features/channelmarket/model-recovery'
import { ShopSettings } from '../features/channelmarket/shop-settings'
import { SupplierAgreementGate } from '../features/policies/acceptance'

export const myChannelsOptions = () =>
  resourceOptions('market-mine', (signal) =>
    api.GET('/api/marketplace/channels/mine', { signal }).then((result) => unwrap(result)),
  )

type Action = 'verify' | 'test' | 'pause' | 'resume' | 'delete' | 'pause-verification'
export default function MarketOwnerPage() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const channels = useSuspenseQuery(myChannelsOptions()).data
  const [tab, setTab] = useState('overview')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('all')
  const visibleChannels = channels.filter(
    (row) =>
      (status === 'all' || row.lifecycle_status === status) &&
      `${row.system_display_name} ${row.id} ${row.provider_type} ${row.declared_models?.join(' ')}`
        .toLowerCase()
        .includes(search.trim().toLowerCase()),
  )
  const [editing, setEditing] = useState<Schema['ChannelMarketChannelView'] | 'new' | null>(null)
  const [selected, setSelected] = useState<Schema['ChannelMarketChannelView'] | null>(null)
  const [inviteToken, setInviteToken] = useState('')
  const [message, setMessage] = useState('')
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ['market-mine'] })
    void client.invalidateQueries({ queryKey: ['market-owner-analytics'] })
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
    <div className="channel-workspace">
      <PageHeader
        title="渠道工作台"
        description="查看服务情况、收入构成与逐笔结算，管理你的供给。"
        action={
          <>
            <Link to="/channel-market" className="button button-secondary">
              {t('浏览市场')}
              <ArrowRight size={16} aria-hidden />
            </Link>
            <Button
              onClick={() => {
                setTab('channels')
                setEditing('new')
                setMessage('')
              }}
            >
              {t('提交渠道')}
            </Button>
          </>
        }
      />
      <ErrorMessage error={save.error ?? action.error ?? invite.error} />
      {message && (
        <p className="notice" role="status">
          {t(message)}
        </p>
      )}
      <Tabs
        label="工作台分区"
        value={tab}
        onValueChange={setTab}
        items={[
          {
            value: 'overview',
            label: '运营概览',
            content: (
              <OwnerAnalytics
                channels={channels}
                onManage={(channel) => {
                  setTab('channels')
                  setSearch(channel ? channel.id : '')
                  setStatus('all')
                }}
              />
            ),
          },
          { value: 'shop', label: '店铺资料', content: <ShopSettings /> },
          {
            value: 'channels',
            label: '渠道与访问',
            content: (
              <div className="channel-workspace-table">
                <div className="market-browser-tools">
                  <label className="field market-search" htmlFor="owner-search">
                    <span>{t('搜索渠道或模型')}</span>
                    <input
                      id="owner-search"
                      value={search}
                      onChange={(event) => setSearch(event.target.value)}
                    />
                  </label>
                  <label className="field" htmlFor="owner-status">
                    <span>{t('服务状态')}</span>
                    <select
                      id="owner-status"
                      value={status}
                      onChange={(event) => setStatus(event.target.value)}
                    >
                      <option value="all">{t('全部状态')}</option>
                      <option value="active">{t('启用')}</option>
                      <option value="paused">{t('已暂停')}</option>
                      <option value="rejected">{t('未通过')}</option>
                      <option value="draft">{t('待验证')}</option>
                    </select>
                  </label>
                </div>
                {editing && (
                  <SupplierAgreementGate required={editing === 'new'}>
                    <MarketChannelForm
                      key={editing === 'new' ? 'new' : editing.id}
                      channel={editing === 'new' ? undefined : editing}
                      pending={save.isPending}
                      onSave={(body, updateService) => save.mutate({ body, updateService })}
                      onCancel={() => setEditing(null)}
                    />
                  </SupplierAgreementGate>
                )}
                {editing && editing !== 'new' && editing.visibility !== 'public' && (
                  <SupplierAgreementGate>
                    <></>
                  </SupplierAgreementGate>
                )}
                <DataTable
                  rows={visibleChannels}
                  rowKey={(row) => row.id}
                  empty="暂无渠道。提交你的上游服务，验证并审核通过后即可供用户使用。"
                  columns={[
                    {
                      label: '渠道',
                      render: (row) => (
                        <>
                          <strong className="channel-workspace-name">
                            {row.system_display_name}
                          </strong>
                          <CopyField value={row.id} label="复制分组 ID" />
                          {row.remark && <p className="muted">{row.remark}</p>}
                          {row.name_status === 'pending' && (
                            <p className="muted">
                              {t('待审名称')} · {row.submitted_name}
                              {row.submitted_remark !== undefined && (
                                <>
                                  <br />
                                  {t('待审备注')} · {row.submitted_remark || t('无')}
                                </>
                              )}
                            </p>
                          )}
                          {row.name_status === 'rejected' && (
                            <p className="muted">
                              {t('名称未通过审核')} · {row.submitted_name}
                              {row.name_review_reason && <> · {row.name_review_reason}</>}
                              {row.submitted_remark !== undefined && (
                                <>
                                  <br />
                                  {t('待审备注')} · {row.submitted_remark || t('无')}
                                </>
                              )}
                            </p>
                          )}
                          <p className="muted">
                            {row.provider_type} ·{' '}
                            {t(row.visibility === 'private' ? '私有渠道' : '公开渠道')}
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
                          {row.last_review_reason && (
                            <p className="muted">{row.last_review_reason}</p>
                          )}
                        </>
                      ),
                    },
                    {
                      label: '操作',
                      render: (row) => (
                        <div className="row-actions">
                          <Button variant="quiet" onClick={() => setEditing(row)}>
                            {t('编辑')}
                          </Button>
                          <Button
                            variant="quiet"
                            onClick={() => {
                              setSelected(row)
                              setInviteToken('')
                            }}
                          >
                            {t('访问管理')}
                          </Button>
                          <Button
                            variant="quiet"
                            disabled={action.isPending}
                            onClick={() => action.mutate({ id: row.id, action: 'verify' })}
                          >
                            {t('验证')}
                          </Button>
                          <Button
                            variant="quiet"
                            disabled={action.isPending}
                            onClick={() => action.mutate({ id: row.id, action: 'test' })}
                          >
                            {t('测试连接')}
                          </Button>
                          {['queued', 'running'].includes(row.verification_status) && (
                            <Button
                              variant="quiet"
                              disabled={action.isPending}
                              onClick={() =>
                                action.mutate({ id: row.id, action: 'pause-verification' })
                              }
                            >
                              {t('暂停验证')}
                            </Button>
                          )}
                          {row.lifecycle_status === 'active' && (
                            <Button
                              variant="quiet"
                              disabled={action.isPending}
                              onClick={() => action.mutate({ id: row.id, action: 'pause' })}
                            >
                              {t('暂停服务')}
                            </Button>
                          )}
                          {row.lifecycle_status === 'paused' && (
                            <Button
                              variant="quiet"
                              disabled={action.isPending}
                              onClick={() => action.mutate({ id: row.id, action: 'resume' })}
                            >
                              {t('恢复服务')}
                            </Button>
                          )}
                          <Button
                            variant="danger"
                            disabled={action.isPending}
                            onClick={() => {
                              void confirmAction({
                                title: `${t('删除渠道「')}${row.system_display_name}${t('」？')}`,
                                danger: true,
                              }).then((ok) => {
                                if (ok) action.mutate({ id: row.id, action: 'delete' })
                              })
                            }}
                          >
                            {t('删除')}
                          </Button>
                        </div>
                      ),
                    },
                  ]}
                />
                {selected && (
                  <section className="section">
                    <div className="page-header">
                      <h2>
                        {selected.system_display_name} {t('· 访问管理')}
                      </h2>
                      <Button variant="quiet" onClick={() => setSelected(null)}>
                        {t('收起')}
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
                      <Field
                        name="invite-expires"
                        label="邀请到期时间（默认 7 天）"
                        type="datetime-local"
                      />
                    </MarketForm>
                    {inviteToken && (
                      <div className="notice" role="status">
                        <p>{t('邀请令牌，仅分享给需要授权的用户：')}</p>
                        <code>{inviteToken}</code>
                      </div>
                    )}
                    <OwnerAccess id={selected.id} internalID={selected.internal_channel_id} />
                    <ModelRecovery
                      id={selected.id}
                      failed={selected.verification_status === 'failed'}
                    />
                  </section>
                )}
              </div>
            ),
          },
          {
            value: 'operations',
            label: '累计收入与结算',
            content: <MarketIncome />,
          },
          {
            value: 'security',
            label: '议价与风控',
            content: (
              <>
                <OwnerBargains />
                <MarketSecurity />
              </>
            ),
          },
        ]}
      />
    </div>
  )
}
