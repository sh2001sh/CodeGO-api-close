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

For commercial licensing, please contact support@quantumnous.com.
*/
import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowRight,
  Check,
  Coins,
  Gift,
  Link2,
  Pause,
  Pencil,
  Play,
  Plus,
  Search,
  ShieldCheck,
  Trash2,
  Upload,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import { formatQuota } from '@/lib/format'
import {
  createMarketplaceGroupInvite,
  createMarketplaceTimeRangeMultiplier,
  deleteMarketplaceChannel,
  deleteMarketplaceTimeRangeMultiplier,
  getMarketplaceTimeRangeMultipliers,
  getMyMarketplaceBargainRequests,
  getMyMarketplaceUserUsage,
  resolveMarketplaceBargainRequest,
  sendMarketplaceBatchWelfare,
  setMarketplaceChannelPaused,
  setMarketplaceUserMultiplier,
} from '@/features/marketplace/api'
import {
  useMarketplaceMutations,
  useMyMarketplaceChannels,
} from '@/features/marketplace/hooks'
import type { MarketplaceChannel } from '@/features/marketplace/types'
import { ChannelEditDialog } from '@/features/marketplace/components/channel-edit-dialog'
import { DawnModal, ModalHead } from '../components/dawn-modal'
import { fmtInt } from '../lib/format'
import { ChannelFormDialog } from './channel-form-dialog'

const LIFECYCLE_LABEL: Record<string, string> = {
  draft: '草稿',
  verifying: '检测中',
  pending_review: '待审核',
  active: '在售',
  degraded: '降级',
  suspended: '已暂停',
  disabled: '已下架',
}

export function OwnerView() {
  const queryClient = useQueryClient()
  const channels = useMyMarketplaceChannels()
  const mutations = useMarketplaceMutations()
  const [keyword, setKeyword] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [editingChannel, setEditingChannel] = useState<MarketplaceChannel | null>(null)
  const [usageChannel, setUsageChannel] = useState<MarketplaceChannel | null>(
    null
  )
  const [ratesChannel, setRatesChannel] = useState<MarketplaceChannel | null>(
    null
  )
  const [inviteChannel, setInviteChannel] = useState<MarketplaceChannel | null>(
    null
  )
  const [confirm, setConfirm] = useState<{
    channel: MarketplaceChannel
    action: 'pause' | 'resume' | 'delete'
  } | null>(null)

  const bargains = useQuery({
    queryKey: ['marketplace-bargains', 'mine'],
    queryFn: () => getMyMarketplaceBargainRequests(''),
    retry: false,
  })

  const list = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    return (channels.data ?? []).filter(
      (channel) =>
        !query ||
        channel.system_display_name.toLowerCase().includes(query) ||
        channel.submitted_source_label.toLowerCase().includes(query)
    )
  }, [channels.data, keyword])

  const totals = useMemo(() => {
    const items = channels.data ?? []
    return {
      requests: items.reduce((sum, channel) => sum + channel.request_count, 0),
      income: items.reduce((sum, channel) => sum + channel.total_income, 0),
      pending: items.reduce((sum, channel) => sum + channel.pending_income, 0),
    }
  }, [channels.data])

  const pendingBargains = (bargains.data?.items ?? []).filter(
    (item) => item.status === 'pending'
  )

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['marketplace-channels'] })
    void queryClient.invalidateQueries({ queryKey: ['marketplace-groups'] })
    void queryClient.invalidateQueries({ queryKey: ['marketplace-bargains'] })
  }

  return (
    <>
      <div className='statband' style={{ marginTop: 26 }}>
        <div className='cell'>
          <b>{fmtInt(totals.requests)}</b>
          <span>请求次数</span>
        </div>
        <div className='cell'>
          <b>{formatQuota(totals.income)}</b>
          <span>累计营收</span>
        </div>
        <div className='cell'>
          <b style={{ color: 'var(--dawn-copper)' }}>
            {formatQuota(totals.pending)}
          </b>
          <span>待结算</span>
        </div>
        <div className='cell'>
          <b>{(channels.data ?? []).length}</b>
          <span>我的分组</span>
        </div>
        <div className='cell'>
          <b style={{ color: 'var(--dawn-warn)' }}>{pendingBargains.length}</b>
          <span>待审砍价</span>
        </div>
      </div>

      {pendingBargains.length > 0 && (
        <div className='gtable' style={{ marginTop: 16 }}>
          <div
            className='tr th'
            style={{
              gridTemplateColumns:
                '110px minmax(0,1.2fr) minmax(0,1fr) 110px minmax(0,1fr) auto',
            }}
          >
            <span>用户</span>
            <span>分组</span>
            <span>现倍率 → 期望</span>
            <span>理由</span>
            <span>提交时间</span>
            <span style={{ textAlign: 'right' }}>操作</span>
          </div>
          {pendingBargains.map((bargain) => (
            <div
              className='tr'
              key={bargain.id}
              style={{
                gridTemplateColumns:
                  '110px minmax(0,1.2fr) minmax(0,1fr) 110px minmax(0,1fr) auto',
                cursor: 'default',
              }}
            >
              <b className='nm'>{bargain.user_external_id}</b>
              <span>{bargain.group_name}</span>
              <span className='num'>
                {bargain.current_multiplier} →{' '}
                <b style={{ color: 'var(--dawn-copper)' }}>
                  {bargain.proposed_multiplier}×
                </b>
              </span>
              <span
                style={{
                  fontSize: 11.5,
                  color: 'var(--dawn-ink2)',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
              >
                {bargain.reason || '—'}
              </span>
              <span className='num' style={{ color: 'var(--dawn-ink2)' }}>
                {new Date(bargain.created_at).toLocaleDateString()}
              </span>
              <span
                style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}
              >
                <button
                  className='btn mini primary'
                  onClick={async () => {
                    try {
                      await resolveMarketplaceBargainRequest({
                        id: bargain.id,
                        action: 'approve',
                        resolutionNote: '',
                      })
                      toast.success(`已接受 · ${bargain.group_name}`)
                      refresh()
                    } catch (error) {
                      toast.error(
                        error instanceof Error ? error.message : '操作失败'
                      )
                    }
                  }}
                >
                  <Check size={13} />
                  接受
                </button>
                <button
                  className='btn mini'
                  onClick={async () => {
                    try {
                      await resolveMarketplaceBargainRequest({
                        id: bargain.id,
                        action: 'reject',
                        resolutionNote: '',
                      })
                      toast.success('已驳回该砍价申请')
                      refresh()
                    } catch (error) {
                      toast.error(
                        error instanceof Error ? error.message : '操作失败'
                      )
                    }
                  }}
                >
                  <X size={13} />
                  驳回
                </button>
              </span>
            </div>
          ))}
        </div>
      )}

      <div className='otool'>
        <button className='btn primary' onClick={() => setShowCreate(true)}>
          <Plus size={14} />
          新建分组 / 上架渠道
        </button>
        <input
          placeholder='搜索我的分组…'
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
        />
      </div>

      <div className='gtable' style={{ marginTop: 16 }}>
        <div className='tr th'>
          <span>分组</span>
          <span>状态</span>
          <span>请求次数</span>
          <span>累计营收</span>
          <span>待结算</span>
          <span />
        </div>
        {list.length ? (
          list.map((channel) => (
            <div
              className='tr'
              key={channel.id}
              onClick={() => setUsageChannel(channel)}
              role='button'
              tabIndex={0}
              onKeyDown={(event) => {
                if (event.key === 'Enter') setUsageChannel(channel)
              }}
            >
              <b className='nm'>
                {channel.system_display_name}
                <span className='ghint'>
                  详情 <ArrowRight size={12} />
                </span>
              </b>
              <span
                className={`num${channel.lifecycle_status === 'active' ? '' : ''}`}
                style={{
                  color:
                    channel.lifecycle_status === 'active'
                      ? 'var(--dawn-ok)'
                      : channel.lifecycle_status === 'suspended'
                        ? 'var(--dawn-warn)'
                        : 'var(--dawn-ink2)',
                }}
              >
                {LIFECYCLE_LABEL[channel.lifecycle_status] ??
                  channel.lifecycle_status}
              </span>
              <span className='num'>{fmtInt(channel.request_count)}</span>
              <span
                className='num'
                style={{ color: 'var(--dawn-copper)', fontWeight: 700 }}
              >
                {formatQuota(channel.total_income)}
              </span>
              <span className='num'>{formatQuota(channel.pending_income)}</span>
              <span
                style={{
                  display: 'flex',
                  gap: 4,
                  flexWrap: 'wrap',
                  justifyContent: 'flex-end',
                }}
                onClick={(event) => event.stopPropagation()}
              >
                <button
                  className='actb'
                  title='编辑渠道'
                  onClick={() => setEditingChannel(channel)}
                >
                  <Pencil size={12} />
                </button>
                <button
                  className='actb'
                  title='时段倍率'
                  onClick={() => setRatesChannel(channel)}
                >
                  <Coins size={12} />
                </button>
                <button
                  className='actb'
                  title='邀请链接'
                  onClick={() => setInviteChannel(channel)}
                >
                  <Link2 size={12} />
                </button>
                <button
                  className='actb'
                  title='检测'
                  onClick={async () => {
                    try {
                      await mutations.detect.mutateAsync(channel.id)
                      toast.success('检测任务已排队')
                      refresh()
                    } catch (error) {
                      toast.error(
                        error instanceof Error ? error.message : '检测失败'
                      )
                    }
                  }}
                >
                  <ShieldCheck size={12} />
                </button>
                <button
                  className='actb'
                  title='连通测试'
                  onClick={async () => {
                    try {
                      await mutations.testConnectivity.mutateAsync(channel.id)
                      toast.success('连通测试已排队')
                      refresh()
                    } catch (error) {
                      toast.error(
                        error instanceof Error ? error.message : '测试失败'
                      )
                    }
                  }}
                >
                  <Upload size={12} />
                </button>
                <button
                  className='actb'
                  title={
                    channel.lifecycle_status === 'suspended' ? '恢复' : '暂停'
                  }
                  onClick={() =>
                    setConfirm({
                      channel,
                      action:
                        channel.lifecycle_status === 'suspended'
                          ? 'resume'
                          : 'pause',
                    })
                  }
                >
                  {channel.lifecycle_status === 'suspended' ? (
                    <Play size={12} />
                  ) : (
                    <Pause size={12} />
                  )}
                </button>
                <button
                  className='actb danger'
                  title='删除'
                  onClick={() => setConfirm({ channel, action: 'delete' })}
                >
                  <Trash2 size={12} />
                </button>
              </span>
            </div>
          ))
        ) : (
          <div className='empty' style={{ marginTop: 0, borderRadius: 0 }}>
            <span className='eic'>
              <Search size={20} />
            </span>
            <b>暂无分组</b>
            <button
              className='btn mini primary'
              onClick={() => setShowCreate(true)}
            >
              <Plus size={13} />
              上架第一个渠道
            </button>
          </div>
        )}
      </div>

      {usageChannel && (
        <UserUsageDialog
          channel={usageChannel}
          onClose={() => setUsageChannel(null)}
        />
      )}
      {ratesChannel && (
        <RatesDialog
          channel={ratesChannel}
          onClose={() => setRatesChannel(null)}
        />
      )}
      {inviteChannel && (
        <InviteDialog
          channel={inviteChannel}
          onClose={() => setInviteChannel(null)}
        />
      )}
      <ChannelFormDialog
        open={showCreate}
        onClose={() => setShowCreate(false)}
      />
      <ChannelEditDialog
        channel={editingChannel}
        open={editingChannel != null}
        onOpenChange={(open) => {
          if (!open) {
            setEditingChannel(null)
            refresh()
          }
        }}
      />
      <DawnModal
        open={!!confirm}
        onClose={() => setConfirm(null)}
        variant='narrow'
        label='确认操作'
      >
        {confirm && (
          <div className='m-main'>
            <ModalHead
              title={
                confirm.action === 'pause'
                  ? '暂停分组'
                  : confirm.action === 'resume'
                    ? '恢复分组'
                    : '删除分组'
              }
              onClose={() => setConfirm(null)}
            />
            <div className='warnicon'>
              {confirm.action === 'delete' ? (
                <Trash2 size={20} color='#b03a2e' />
              ) : confirm.action === 'pause' ? (
                <Pause size={20} />
              ) : (
                <Play size={20} />
              )}
            </div>
            <div className='kv'>
              <span>分组</span>
              <b>{confirm.channel.system_display_name}</b>
            </div>
            <div className='kv'>
              <span>待结算</span>
              <b>{formatQuota(confirm.channel.pending_income)}</b>
            </div>
            <div className='m-foot'>
              <button className='btn' onClick={() => setConfirm(null)}>
                取消
              </button>
              <button
                className='btn primary'
                onClick={async () => {
                  try {
                    if (confirm.action === 'delete') {
                      await deleteMarketplaceChannel(confirm.channel.id, false)
                    } else {
                      await setMarketplaceChannelPaused(
                        confirm.channel.id,
                        confirm.action === 'pause'
                      )
                    }
                    toast.success(
                      confirm.action === 'delete'
                        ? '已删除'
                        : confirm.action === 'pause'
                          ? '已暂停'
                          : '已恢复'
                    )
                    setConfirm(null)
                    refresh()
                  } catch (error) {
                    toast.error(
                      error instanceof Error ? error.message : '操作失败'
                    )
                  }
                }}
              >
                确认
              </button>
            </div>
          </div>
        )}
      </DawnModal>
    </>
  )
}

/** 用户使用情况 + 特殊倍率 + 批量福利。 */
function UserUsageDialog(props: {
  channel: MarketplaceChannel
  onClose: () => void
}) {
  const { channel } = props
  const [sort, setSort] = useState<'req' | 'spend'>('req')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [rateUser, setRateUser] = useState<{
    id: string
    name: string
    multiplier?: number
  } | null>(null)
  const [welfare, setWelfare] = useState<'transfer' | 'blind_box' | null>(null)

  const usage = useQuery({
    queryKey: ['marketplace-user-usage', channel.id],
    queryFn: () => getMyMarketplaceUserUsage(channel.id),
    retry: false,
    staleTime: 15_000,
    refetchOnWindowFocus: false,
  })

  const rows = useMemo(() => {
    const items = (usage.data?.items ?? []).filter(
      (item) => item.channel_id === channel.id
    )
    return items.sort((a, b) =>
      sort === 'req'
        ? b.request_count - a.request_count
        : b.total_consumer_amount - a.total_consumer_amount
    )
  }, [usage.data, channel.id, sort])

  const toggle = (id: string) =>
    setSelected((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  return (
    <>
      <DawnModal open onClose={props.onClose} label='用户使用情况'>
        <div className='m-main'>
          <ModalHead
            title={channel.system_display_name}
            onClose={props.onClose}
          />
          <div
            className='metrics'
            style={{ gridTemplateColumns: 'repeat(4,1fr)', marginTop: 0 }}
          >
            <div className='m'>
              <b>
                {fmtInt(rows.reduce((sum, row) => sum + row.request_count, 0))}
              </b>
              <span>请求次数</span>
            </div>
            <div className='m'>
              <b style={{ color: 'var(--dawn-copper)' }}>
                {formatQuota(
                  rows.reduce((sum, row) => sum + row.total_consumer_amount, 0)
                )}
              </b>
              <span>消耗额度</span>
            </div>
            <div className='m'>
              <b>{rows.length}</b>
              <span>用户</span>
            </div>
            <div className='m good'>
              <b>
                {rows.length
                  ? `${(
                      (rows.reduce((sum, row) => sum + row.success_count, 0) /
                        Math.max(
                          1,
                          rows.reduce((sum, row) => sum + row.request_count, 0)
                        )) *
                      100
                    ).toFixed(1)}%`
                  : '—'}
              </b>
              <span>成功率</span>
            </div>
          </div>
          <div
            style={{
              display: 'flex',
              gap: 10,
              alignItems: 'center',
              margin: '16px 0 10px',
              flexWrap: 'wrap',
            }}
          >
            <div className='kick' style={{ fontSize: 10 }}>
              <span className='n'>RANK</span>
              用户排名
            </div>
            <span style={{ flex: 1 }} />
            <select
              className='fsel'
              value={sort}
              onChange={(event) =>
                setSort(event.target.value as 'req' | 'spend')
              }
            >
              <option value='req'>按请求次数</option>
              <option value='spend'>按消耗额度</option>
            </select>
          </div>
          <div className='gtable usertable'>
            <div className='tr th'>
              <span />
              <span>用户</span>
              <span>请求次数</span>
              <span>消耗额度</span>
              <span>成功率</span>
              <span />
            </div>
            {rows.length ? (
              rows.map((row, index) => (
                <label
                  className='tr usertable'
                  style={{ display: 'grid' }}
                  key={row.user_id}
                >
                  <input
                    type='checkbox'
                    checked={selected.has(row.user_id)}
                    onClick={(event) => event.stopPropagation()}
                    onChange={() => toggle(row.user_id)}
                  />
                  <b className='nm' style={{ fontWeight: 700 }}>
                    <span className='rankn'>
                      {String(index + 1).padStart(2, '0')}
                    </span>
                    {row.external_user_id || row.user_id}
                    {row.user_multiplier != null && (
                      <span className='rtag'>{row.user_multiplier}×</span>
                    )}
                  </b>
                  <span className='num'>{fmtInt(row.request_count)}</span>
                  <span
                    className='num'
                    style={{ color: 'var(--dawn-copper)', fontWeight: 700 }}
                  >
                    {formatQuota(row.total_consumer_amount)}
                  </span>
                  <span className='num'>
                    {(row.success_rate * 100).toFixed(1)}%
                  </span>
                  <button
                    className='actb'
                    onClick={(event) => {
                      event.preventDefault()
                      event.stopPropagation()
                      setRateUser({
                        id: row.user_id,
                        name: row.external_user_id || row.user_id,
                        multiplier: row.user_multiplier,
                      })
                    }}
                  >
                    <Pencil size={12} />
                    倍率
                  </button>
                </label>
              ))
            ) : (
              <div className='prev-empty'>暂无用户数据</div>
            )}
          </div>
        </div>
        <div className='m-rail'>
          <h5>
            <Coins size={14} />
            渠道收入
          </h5>
          <div className='rsub'>SETTLEMENT</div>
          <div className='kv'>
            <span>累计营收</span>
            <b>{formatQuota(channel.total_income)}</b>
          </div>
          <div className='kv'>
            <span>待结算</span>
            <b>{formatQuota(channel.pending_income)}</b>
          </div>
          <div className='kv'>
            <span>已释放</span>
            <b>{formatQuota(channel.released_income)}</b>
          </div>
          <div className='rule'>
            <span className='ric'>
              <Coins size={14} />
            </span>
            <div>
              <b>95% 渠道收入</b>
            </div>
          </div>
          <div className='rule'>
            <span className='ric'>
              <ShieldCheck size={14} />
            </span>
            <div>
              <b>检测通过自动上架</b>
            </div>
          </div>
        </div>
      </DawnModal>

      {selected.size > 0 && (
        <div className='batchbar on'>
          <span style={{ fontFamily: 'var(--dawn-mono)', fontSize: 12 }}>
            已选 {selected.size} 用户
          </span>
          <button
            className='btn mini night'
            onClick={() => setWelfare('transfer')}
          >
            <Coins size={13} />
            批量转账
          </button>
          <button
            className='btn mini night'
            onClick={() => setWelfare('blind_box')}
          >
            <Gift size={13} />
            发盲盒
          </button>
          <button
            className='btn mini night'
            onClick={() => setSelected(new Set())}
          >
            <X size={13} />
          </button>
        </div>
      )}

      <DawnModal
        open={!!rateUser}
        onClose={() => setRateUser(null)}
        variant='narrow'
        label='用户特殊倍率'
      >
        {rateUser && (
          <div className='m-main'>
            <ModalHead title='用户特殊倍率' onClose={() => setRateUser(null)} />
            <div className='kv'>
              <span>用户</span>
              <b>{rateUser.name}</b>
            </div>
            <div className='kv'>
              <span>当前倍率</span>
              <b>
                {rateUser.multiplier != null
                  ? `${rateUser.multiplier}×`
                  : `${channel.multiplier}×（跟随分组）`}
              </b>
            </div>
            <div className='field' style={{ marginTop: 12 }}>
              <label>特殊倍率</label>
              <input
                id='user-rate-input'
                defaultValue={rateUser.multiplier ?? 0.8}
                type='number'
                step='0.05'
                min='0.1'
              />
            </div>
            <div className='m-foot'>
              {rateUser.multiplier != null && (
                <button
                  className='btn'
                  onClick={async () => {
                    try {
                      await setMarketplaceUserMultiplier({
                        channelId: channel.id,
                        userId: Number(rateUser.id),
                        multiplier: null,
                      })
                      toast.success('已恢复跟随分组')
                      setRateUser(null)
                      void usage.refetch()
                    } catch (error) {
                      toast.error(
                        error instanceof Error ? error.message : '操作失败'
                      )
                    }
                  }}
                >
                  恢复默认
                </button>
              )}
              <button
                className='btn primary'
                onClick={async () => {
                  const input = document.querySelector(
                    '#user-rate-input'
                  ) as HTMLInputElement | null
                  const value = Number(input?.value ?? '1')
                  try {
                    await setMarketplaceUserMultiplier({
                      channelId: channel.id,
                      userId: Number(rateUser.id),
                      multiplier:
                        Number.isFinite(value) && value > 0 ? value : null,
                    })
                    toast.success('特殊倍率已生效')
                    setRateUser(null)
                    void usage.refetch()
                  } catch (error) {
                    toast.error(
                      error instanceof Error ? error.message : '操作失败'
                    )
                  }
                }}
              >
                生效
              </button>
            </div>
          </div>
        )}
      </DawnModal>

      <DawnModal
        open={!!welfare}
        onClose={() => setWelfare(null)}
        variant='narrow'
        label='批量福利'
      >
        {welfare && (
          <div className='m-main'>
            <ModalHead
              title={welfare === 'transfer' ? '批量转账' : '发盲盒'}
              onClose={() => setWelfare(null)}
            />
            <div className='field'>
              <label>
                {welfare === 'transfer' ? '每人额度（$）' : '盲盒金额（$）'}
              </label>
              <input
                id='welfare-amount'
                defaultValue='1'
                type='number'
                min='0.01'
                step='0.5'
              />
            </div>
            <div className='m-foot'>
              <button className='btn' onClick={() => setWelfare(null)}>
                取消
              </button>
              <button
                className='btn primary'
                onClick={async () => {
                  const input = document.querySelector(
                    '#welfare-amount'
                  ) as HTMLInputElement | null
                  const amount = Number(input?.value ?? '1') || 1
                  try {
                    const result = await sendMarketplaceBatchWelfare({
                      channelId: channel.id,
                      userIds: [...selected],
                      type: welfare,
                      amount,
                    })
                    toast.success(`已发放 ${result.success_count} 位用户`)
                    setWelfare(null)
                    setSelected(new Set())
                  } catch (error) {
                    toast.error(
                      error instanceof Error ? error.message : '操作失败'
                    )
                  }
                }}
              >
                {welfare === 'transfer' ? (
                  <Coins size={13} />
                ) : (
                  <Gift size={13} />
                )}
                确认发放
              </button>
            </div>
          </div>
        )}
      </DawnModal>
    </>
  )
}

/** 时段倍率管理。 */
function RatesDialog(props: {
  channel: MarketplaceChannel
  onClose: () => void
}) {
  const { channel } = props
  const rules = useQuery({
    queryKey: ['marketplace-time-range-multipliers', channel.id],
    queryFn: () => getMarketplaceTimeRangeMultipliers(channel.id),
    retry: false,
  })
  const [draft, setDraft] = useState({
    start: '22:00',
    end: '06:00',
    rate: '0.8',
  })

  const today = new Date()
  const toTimestamp = (value: string, dayOffset = 0) => {
    const [hours, minutes] = value.split(':').map(Number)
    const date = new Date(today)
    date.setDate(date.getDate() + dayOffset)
    date.setHours(hours || 0, minutes || 0, 0, 0)
    return Math.floor(date.getTime() / 1000)
  }

  return (
    <DawnModal open onClose={props.onClose} label='时段倍率'>
      <div className='m-main'>
        <ModalHead
          title={`时段倍率 · ${channel.system_display_name}`}
          onClose={props.onClose}
        />
        <div className='gtable' style={{ marginTop: 0 }}>
          <div
            className='tr th'
            style={{ gridTemplateColumns: 'minmax(0,1fr) 100px 100px 60px' }}
          >
            <span>时段</span>
            <span>倍率</span>
            <span>标注</span>
            <span />
          </div>
          {(rules.data ?? []).length ? (
            rules.data!.map((rule) => (
              <div
                className='tr'
                key={rule.id}
                style={{
                  gridTemplateColumns: 'minmax(0,1fr) 100px 100px 60px',
                  cursor: 'default',
                }}
              >
                <span className='num'>
                  {new Date(
                    rule.start_timestamp < 1e12
                      ? rule.start_timestamp * 1000
                      : rule.start_timestamp
                  ).toLocaleTimeString([], {
                    hour: '2-digit',
                    minute: '2-digit',
                  })}
                  {' – '}
                  {new Date(
                    rule.end_timestamp < 1e12
                      ? rule.end_timestamp * 1000
                      : rule.end_timestamp
                  ).toLocaleTimeString([], {
                    hour: '2-digit',
                    minute: '2-digit',
                  })}
                </span>
                <span className='num'>
                  <b style={{ color: 'var(--dawn-copper)' }}>
                    {rule.multiplier}×
                  </b>
                </span>
                <span className='num' style={{ color: 'var(--dawn-ink2)' }}>
                  {rule.label || '—'}
                </span>
                <span style={{ display: 'flex', justifyContent: 'flex-end' }}>
                  <button
                    className='actb danger'
                    onClick={async () => {
                      try {
                        await deleteMarketplaceTimeRangeMultiplier({
                          channelId: channel.id,
                          ruleId: rule.id,
                        })
                        toast.success('时段规则已删除')
                        void rules.refetch()
                      } catch (error) {
                        toast.error(
                          error instanceof Error ? error.message : '删除失败'
                        )
                      }
                    }}
                  >
                    <Trash2 size={12} />
                  </button>
                </span>
              </div>
            ))
          ) : (
            <div className='prev-empty'>暂无时段规则</div>
          )}
        </div>
        <div className='rrow' style={{ marginTop: 14 }}>
          <input
            type='time'
            value={draft.start}
            onChange={(event) =>
              setDraft((d) => ({ ...d, start: event.target.value }))
            }
          />
          <input
            type='time'
            value={draft.end}
            onChange={(event) =>
              setDraft((d) => ({ ...d, end: event.target.value }))
            }
          />
          <input
            type='number'
            step='0.05'
            min='0.1'
            value={draft.rate}
            onChange={(event) =>
              setDraft((d) => ({ ...d, rate: event.target.value }))
            }
          />
          <button
            className='btn mini primary'
            style={{ borderRadius: 8 }}
            onClick={async () => {
              const start = toTimestamp(draft.start)
              let end = toTimestamp(draft.end)
              if (end <= start) end = toTimestamp(draft.end, 1)
              try {
                await createMarketplaceTimeRangeMultiplier({
                  channelId: channel.id,
                  startTimestamp: start,
                  endTimestamp: end,
                  multiplier: Number(draft.rate) || 1,
                  label: '',
                })
                toast.success('时段倍率已保存')
                void rules.refetch()
              } catch (error) {
                toast.error(error instanceof Error ? error.message : '保存失败')
              }
            }}
          >
            <Plus size={13} />
            添加
          </button>
        </div>
      </div>
    </DawnModal>
  )
}

/** 邀请链接。 */
function InviteDialog(props: {
  channel: MarketplaceChannel
  onClose: () => void
}) {
  const { channel } = props
  const invite = useQuery({
    queryKey: ['marketplace-invite', channel.group_id],
    queryFn: () => createMarketplaceGroupInvite(channel.group_id),
    retry: false,
  })
  const link = invite.data
    ? `${window.location.origin}/market?invite=${invite.data.token}`
    : ''

  return (
    <DawnModal open onClose={props.onClose} variant='narrow' label='邀请链接'>
      <div className='m-main'>
        <ModalHead title='邀请链接' onClose={props.onClose} />
        <div className='field'>
          <label>链接</label>
          <div style={{ display: 'flex', gap: 8 }}>
            <input
              readOnly
              value={invite.isLoading ? '…' : link}
              style={{ flex: 1, fontFamily: 'var(--dawn-mono)', fontSize: 12 }}
            />
            <button
              className='btn'
              onClick={() => {
                void navigator.clipboard?.writeText(link)
                toast.success('已复制')
              }}
            >
              复制
            </button>
          </div>
          {invite.isError ? (
            <span style={{ color: 'var(--dawn-bad)', fontSize: 12 }}>
              邀请链接生成失败，请关闭后重试
            </span>
          ) : null}
        </div>
        <div className='kv'>
          <span>分组</span>
          <b>{channel.system_display_name}</b>
        </div>
        <div className='m-foot'>
          <button className='btn' onClick={props.onClose}>
            关闭
          </button>
          <button
            className='btn primary'
            disabled={!link}
            onClick={() => {
              void navigator.clipboard?.writeText(link)
              toast.success('已复制')
              props.onClose()
            }}
          >
            复制链接
          </button>
        </div>
      </div>
    </DawnModal>
  )
}
