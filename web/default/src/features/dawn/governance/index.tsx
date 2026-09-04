/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useMemo, useState } from 'react'
import {
  FileSearch,
  MailWarning,
  RefreshCw,
  ShieldCheck,
  Store,
  Trash2,
} from 'lucide-react'
import { toast } from 'sonner'
import { SiteSeo } from '@/components/seo'
import { DawnModal, ModalHead } from '@/features/dawn/components/dawn-modal'
import { DawnQueryError } from '@/features/dawn/components/query-error'
import {
  useAdminMarketplaceChannels,
  useAdminMarketplaceReview,
  useMarketplaceMutations,
} from '@/features/marketplace/hooks'
import type { MarketplaceChannel } from '@/features/marketplace/types'

const STATUS_LABEL: Record<string, string> = {
  draft: '草稿',
  verifying: '检测中',
  pending_review: '待审核',
  active: '在售',
  degraded: '质量下降',
  suspended: '已暂停',
  disabled: '已停用',
}

export function DawnMarketplaceGovernance() {
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState('')
  const [detail, setDetail] = useState<MarketplaceChannel | null>(null)
  const query = useAdminMarketplaceChannels(
    { search: search.trim(), status },
    true
  )
  const review = useAdminMarketplaceReview()
  const mutations = useMarketplaceMutations()

  const channels = useMemo(() => query.data ?? [], [query.data])
  const counts = useMemo(() => {
    const active = channels.filter(
      (c) => c.lifecycle_status === 'active'
    ).length
    const pending = channels.filter(
      (c) => c.lifecycle_status === 'pending_review'
    ).length
    const suspended = channels.filter(
      (c) =>
        c.lifecycle_status === 'suspended' || c.lifecycle_status === 'disabled'
    ).length
    return { active, pending, suspended, total: channels.length }
  }, [channels])

  const notify = (channel: MarketplaceChannel) => {
    review.mutate(
      {
        id: channel.id,
        approved: false,
        reason: '通知整改：请按平台规范调整分组参数与模型能力。',
      },
      {
        onSuccess: () =>
          toast.success(`已通知整改：${channel.system_display_name}`),
        onError: (error) =>
          toast.error(error instanceof Error ? error.message : '通知整改失败'),
      }
    )
  }

  const togglePause = (channel: MarketplaceChannel) => {
    const paused = channel.lifecycle_status !== 'active'
    mutations.adminPause.mutate(
      { id: channel.id, paused },
      {
        onSuccess: () =>
          toast.success(
            paused
              ? `已强制下架：${channel.system_display_name}`
              : `已恢复在售：${channel.system_display_name}`
          ),
        onError: (error) =>
          toast.error(error instanceof Error ? error.message : '操作失败'),
      }
    )
  }

  return (
    <div className='dawn-governance'>
      <SiteSeo
        title='市场分组治理 | Code Go'
        description='市场分组治理'
        canonicalPath='/marketplace'
        robots='noindex,follow'
      />
      <div className='pgw'>
        <div>
          <div className='kick'>
            <span className='n'>A·01</span>
            MARKET GROUPS
          </div>
          <h1 className='pg'>市场分组治理</h1>
        </div>
        <button
          className='btn mini'
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
        >
          <RefreshCw
            size={14}
            className={query.isFetching ? 'animate-spin' : ''}
          />
          刷新
        </button>
      </div>

      <div className='statband'>
        <div className='cell'>
          <b>{counts.active}</b>
          <span>在售分组</span>
        </div>
        <div className='cell'>
          <b className='c-warn'>{counts.pending}</b>
          <span>待审核</span>
        </div>
        <div className='cell'>
          <b style={{ color: 'var(--dawn-bad)' }}>{counts.suspended}</b>
          <span>已下架 / 暂停</span>
        </div>
        <div className='cell'>
          <b>{counts.total}</b>
          <span>渠道总数</span>
        </div>
      </div>

      <div className='filters'>
        <input
          placeholder='搜索分组 / 渠道主 / 来源'
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <select
          className='fsel'
          value={status}
          onChange={(event) => setStatus(event.target.value)}
        >
          <option value=''>全部状态</option>
          {Object.entries(STATUS_LABEL).map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </div>

      {query.isLoading ? (
        <div className='empty'>
          <span className='eic'>
            <Store size={20} className='animate-pulse' />
          </span>
          <b>治理数据加载中</b>
        </div>
      ) : query.isError ? (
        <DawnQueryError
          title='治理数据加载失败'
          description='管理员渠道数据暂时不可用，请稍后重试。'
          onRetry={() => void query.refetch()}
          retrying={query.isFetching}
        />
      ) : channels.length === 0 ? (
        <div className='empty'>
          <span className='eic'>
            <ShieldCheck size={20} />
          </span>
          <b>当前没有待治理分组</b>
        </div>
      ) : (
        <div className='gtable' style={{ marginTop: 18 }}>
          <div
            className='tr th'
            style={{
              gridTemplateColumns:
                'minmax(0,1.4fr) 110px 90px 100px 100px minmax(0,1.2fr)',
            }}
          >
            <span>分组</span>
            <span>渠道主</span>
            <span>订阅</span>
            <span>待审砍价</span>
            <span>状态</span>
            <span style={{ textAlign: 'right' }}>操作</span>
          </div>
          {channels.map((channel) => (
            <div
              className='tr'
              key={channel.id}
              style={{
                gridTemplateColumns:
                  'minmax(0,1.4fr) 110px 90px 100px 100px minmax(0,1.2fr)',
              }}
            >
              <b className='nm'>
                <Store size={14} color='var(--dawn-copper)' />
                {channel.system_display_name}
              </b>
              <span className='num'>{channel.owner_external_id || '—'}</span>
              <span className='num'>—</span>
              <span className='num'>—</span>
              <span className='num'>
                <b
                  style={{
                    color:
                      channel.lifecycle_status === 'active'
                        ? 'var(--dawn-ok)'
                        : 'var(--dawn-ink2)',
                  }}
                >
                  {STATUS_LABEL[channel.lifecycle_status] ??
                    channel.lifecycle_status}
                </b>
              </span>
              <span
                style={{
                  display: 'flex',
                  gap: 6,
                  justifyContent: 'flex-end',
                  flexWrap: 'wrap',
                }}
              >
                <button className='actb' onClick={() => setDetail(channel)}>
                  <FileSearch size={12} />
                  治理详情
                </button>
                <button className='actb' onClick={() => notify(channel)}>
                  <MailWarning size={12} />
                  通知整改
                </button>
                <button
                  className='actb danger'
                  onClick={() => togglePause(channel)}
                >
                  {channel.lifecycle_status === 'active' ? (
                    <>
                      <Trash2 size={12} />
                      强制下架
                    </>
                  ) : (
                    <>
                      <ShieldCheck size={12} />
                      恢复上架
                    </>
                  )}
                </button>
              </span>
            </div>
          ))}
        </div>
      )}

      <DawnModal
        open={detail != null}
        onClose={() => setDetail(null)}
        variant='narrow'
        label='治理详情'
      >
        {detail && (
          <div className='m-main'>
            <ModalHead
              title={`治理详情 · ${detail.system_display_name}`}
              onClose={() => setDetail(null)}
            />
            <div className='kv'>
              <span>分组 ID</span>
              <b>{detail.id}</b>
            </div>
            <div className='kv'>
              <span>渠道主</span>
              <b>{detail.owner_external_id || '—'}</b>
            </div>
            <div className='kv'>
              <span>协议</span>
              <b>{detail.provider_type}</b>
            </div>
            <div className='kv'>
              <span>来源</span>
              <b>
                {detail.approved_source_label || detail.submitted_source_label}
              </b>
            </div>
            <div className='kv'>
              <span>倍率</span>
              <b>{detail.multiplier.toFixed(2)}×</b>
            </div>
            <div className='kv'>
              <span>模型数</span>
              <b>{detail.declared_models.length} 个</b>
            </div>
            <div className='kv'>
              <span>状态</span>
              <b>
                {STATUS_LABEL[detail.lifecycle_status] ??
                  detail.lifecycle_status}
              </b>
            </div>
            <div className='kv'>
              <span>结算请求</span>
              <b>{detail.request_count.toLocaleString()}</b>
            </div>
            <div className='kv'>
              <span>累计收益</span>
              <b>{detail.total_income.toLocaleString()}</b>
            </div>
          </div>
        )}
      </DawnModal>
    </div>
  )
}
