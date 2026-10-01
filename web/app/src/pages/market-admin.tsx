import { useState } from 'react'
import { useMutation, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import type { Schema } from '../lib/types'
import { DataTable } from '../components/data-table'
import { Button, ErrorMessage, Field, PageHeader, Status } from '../components/ui'
import { MarketForm, text } from '../features/channelmarket/form'
import { MarketIncome, MarketSecurity } from '../features/channelmarket/reports'
import { MarketChannelForm, channelPatch } from '../features/channelmarket/channel-form'

export const marketAdminOptions = () =>
  resourceOptions('market-admin-channels', (signal) =>
    api.GET('/api/marketplace/admin/channels', { signal }).then((result) => unwrap(result)),
  )

export default function MarketAdminPage() {
  const client = useQueryClient()
  const rows = useSuspenseQuery(marketAdminOptions()).data
  const [selected, setSelected] = useState<Schema['ChannelMarketChannelView'] | null>(null)
  const [editing, setEditing] = useState<Schema['ChannelMarketChannelView'] | null>(null)
  const [filter, setFilter] = useState('')
  const refresh = () => {
    setSelected(null)
    void client.invalidateQueries({ queryKey: ['market-admin-channels'] })
  }
  const review = useMutation({
    mutationFn: (body: { approved: boolean; reason: string }) =>
      api.POST('/api/marketplace/admin/channels/{id}/review', {
        params: { path: { id: selected!.id } },
        body,
      }),
    onSuccess: refresh,
  })
  const save = useMutation({
    mutationFn: (input: { body: Schema['ChannelMarketCreateInput']; updateService: boolean }) =>
      api.PATCH('/api/marketplace/admin/channels/{id}', {
        params: { path: { id: editing!.id } },
        body: channelPatch(input.body, editing!, input.updateService),
      }),
    onSuccess: () => {
      setEditing(null)
      refresh()
    },
  })
  const action = useMutation({
    mutationFn: (input: {
      id: string
      action: 'verify' | 'test' | 'pause' | 'resume' | 'delete'
    }) => {
      const params = { path: { id: input.id } }
      if (input.action === 'verify')
        return api.POST('/api/marketplace/admin/channels/{id}/verify', { params })
      if (input.action === 'test')
        return api.POST('/api/marketplace/admin/channels/{id}/test', { params })
      if (input.action === 'pause')
        return api.POST('/api/marketplace/admin/channels/{id}/pause', { params })
      if (input.action === 'resume')
        return api.POST('/api/marketplace/admin/channels/{id}/resume', { params })
      return api.DELETE('/api/marketplace/admin/channels/{id}', { params })
    },
    onSuccess: refresh,
  })
  return (
    <>
      <PageHeader title="渠道市场审核" />
      <ErrorMessage error={review.error ?? action.error ?? save.error} />
      {editing && (
        <MarketChannelForm
          key={editing.id}
          channel={editing}
          pending={save.isPending}
          onSave={(body, updateService) => save.mutate({ body, updateService })}
          onCancel={() => setEditing(null)}
        />
      )}
      <label className="field filters" htmlFor="review-filter">
        <span>渠道状态</span>
        <select
          id="review-filter"
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
        >
          <option value="">全部</option>
          <option value="draft">待验证</option>
          <option value="verifying">验证中 / 待审核</option>
          <option value="active">服务中</option>
          <option value="paused">已暂停</option>
          <option value="rejected">未通过</option>
        </select>
      </label>
      <DataTable
        rows={rows.filter((row) => !filter || row.lifecycle_status === filter)}
        rowKey={(row) => row.id}
        columns={[
          {
            label: '渠道',
            render: (row) => (
              <>
                {row.system_display_name}
                <p className="muted">渠道主 {String(row.owner_user_id)}</p>
              </>
            ),
          },
          { label: '模型', render: (row) => row.declared_models?.join(', ') },
          { label: '状态', render: (row) => <Status value={row.lifecycle_status} /> },
          { label: '验证', render: (row) => <Status value={row.verification_status} /> },
          {
            label: '操作',
            render: (row) => (
              <div className="row-actions">
                <Button variant="quiet" onClick={() => setEditing(row)}>
                  编辑
                </Button>
                <Button variant="quiet" onClick={() => setSelected(row)}>
                  审核
                </Button>
                <Button
                  variant="quiet"
                  disabled={action.isPending}
                  onClick={() => action.mutate({ id: row.id, action: 'test' })}
                >
                  测试连接
                </Button>
                <Button
                  variant="quiet"
                  disabled={action.isPending}
                  onClick={() => action.mutate({ id: row.id, action: 'verify' })}
                >
                  重新验证
                </Button>
                {row.lifecycle_status === 'active' && (
                  <Button
                    variant="quiet"
                    disabled={action.isPending}
                    onClick={() => action.mutate({ id: row.id, action: 'pause' })}
                  >
                    暂停
                  </Button>
                )}
                {row.lifecycle_status === 'paused' && (
                  <Button
                    variant="quiet"
                    disabled={action.isPending}
                    onClick={() => action.mutate({ id: row.id, action: 'resume' })}
                  >
                    恢复
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
          <h2>审核 · {selected.system_display_name}</h2>
          <p className="muted">验证通过后可批准发布；未通过时填写原因。</p>
          <MarketForm
            pending={review.isPending}
            submit="提交审核"
            onSubmit={(fields) =>
              review.mutate({
                approved: text(fields, 'decision') === 'approve',
                reason: text(fields, 'review-reason'),
              })
            }
          >
            <label className="field" htmlFor="review-decision">
              <span>审核结果</span>
              <select id="review-decision" name="decision" defaultValue="reject">
                <option value="reject">不通过</option>
                <option value="approve" disabled={selected.verification_status !== 'passed'}>
                  批准发布
                </option>
              </select>
            </label>
            <Field name="review-reason" label="审核理由" required maxLength={1000} />
            <Button type="button" variant="quiet" onClick={() => setSelected(null)}>
              取消
            </Button>
          </MarketForm>
        </section>
      )}
      <MarketIncome admin />
      <MarketSecurity admin />
    </>
  )
}
