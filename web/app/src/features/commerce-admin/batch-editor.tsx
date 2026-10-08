import { useState } from 'react'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions, sessionOptions } from '../../lib/queries'
import { credits, date } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { Button, ErrorMessage, Loading, confirmAction } from '../../components/ui'
import { DataTable } from '../../components/data-table'
import type { BoxBatch, BoxBatchReward, BoxBatchStats } from '../blind-box/batch-contract'
import {
  batchReservePreview,
  boxInteger,
  persistentBoxOperations,
} from '../blind-box/batch-presentation'
import { weightPercentage } from '../blind-box/presentation'
import { BoxPlanSpecification } from '../blind-box/plan-specification'
import { decimalCredits, nonnegativeMicroCredits } from '../commerce/amounts'
import { newBatchReward, newBoxBatch, validateBatchDraft } from './batch-draft'
import { poolConfigJSON } from './pool-draft'
import '../blind-box/blind-box.css'

export const adminBoxBatchesOptions = () =>
  resourceOptions<BoxBatch[]>('admin-box-batches', (signal) =>
    api.GET('/api/blind-box/admin/batches', { signal }).then(unwrap),
  )

const batchStateLabel = (state: BoxBatch['state']) =>
  ({ draft: '草稿', published: '已发布', paused: '已停售', exhausted: '已发完' })[state]

export function BoxBatchEditor() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const { data: user } = useSuspenseQuery(sessionOptions())
  const query = useQuery(adminBoxBatchesOptions())
  const plans = useQuery(
    resourceOptions('admin-plans', (signal) =>
      api.GET('/api/subscription/admin/plans', { signal }).then(unwrap),
    ),
  )
  const [editing, setEditing] = useState<BoxBatch | null>(null)
  const [savedConfiguration, setSavedConfiguration] = useState('')
  const [statsID, setStatsID] = useState('')
  const [error, setError] = useState<Error | null>(null)
  const [confirming, setConfirming] = useState(false)
  const stats = useQuery({
    ...resourceOptions<BoxBatchStats>(
      'box-batch-stats',
      (signal) =>
        api
          .GET('/api/blind-box/admin/batches/{id}/stats', {
            signal,
            params: { path: { id: statsID } },
          })
          .then(unwrap),
      [statsID],
    ),
    enabled: !!statsID,
  })
  const refresh = () => {
    void client.invalidateQueries({ queryKey: ['admin-box-batches'] })
    void client.invalidateQueries({ queryKey: ['box-batches'] })
    void client.invalidateQueries({ queryKey: ['wallet'] })
    void client.invalidateQueries({ queryKey: ['box-batch-stats'] })
  }
  const save = useMutation({
    mutationFn: (draft: BoxBatch): Promise<BoxBatch> =>
      api.PUT('/api/blind-box/admin/batches', { body: draft }).then(unwrap),
    onSuccess: (batch) => {
      setEditing(batch)
      setSavedConfiguration(poolConfigJSON(batch))
      refresh()
    },
  })
  const changeState = useMutation({
    mutationFn: ({
      batch,
      action,
    }: {
      batch: BoxBatch
      action: 'publish' | 'pause'
    }): Promise<BoxBatch> => {
      const operations = persistentBoxOperations(String(user.id), sessionStorage)
      const key = `${action}:${batch.id}:${batch.revision}`
      return api
        .POST(
          action === 'publish'
            ? '/api/blind-box/admin/batches/{id}/publish'
            : '/api/blind-box/admin/batches/{id}/pause',
          {
            params: { path: { id: String(batch.id) } },
            body: { request_id: operations.forPayload(key), revision: batch.revision },
          },
        )
        .then(unwrap)
    },
    onSuccess: (batch, input) => {
      persistentBoxOperations(String(user.id), sessionStorage).complete(
        `${input.action}:${input.batch.id}:${input.batch.revision}`,
      )
      setEditing(batch)
      setSavedConfiguration(poolConfigJSON(batch))
      refresh()
    },
  })
  const pending = save.isPending || changeState.isPending || confirming
  const immutable = editing !== null && editing.state !== 'draft'
  const dirty = editing !== null && poolConfigJSON(editing) !== savedConfiguration
  const reviewState = async (batch: BoxBatch, action: 'publish' | 'pause') => {
    if (pending) return
    setConfirming(true)
    setError(null)
    try {
      const accepted = await confirmAction({
        title: t(action === 'publish' ? '确认发布并锁定准备金？' : '确认停止新发放？'),
        description:
          action === 'publish'
            ? `${t('将从管理员钱包扣除并锁定准备金')} ${credits(batch.required_budget_micro)}。${t('发布后价格、数量和奖励规格不可修改；已承诺的权益继续履约。')}`
            : t('停止新的购买与资格发放；已有免费领取资格和已获得权益继续履约。'),
        confirmLabel: action === 'publish' ? '发布并锁定准备金' : '停止新发放',
      })
      if (accepted) changeState.mutate({ batch, action })
    } finally {
      setConfirming(false)
    }
  }
  const previewBatch = editing && {
    ...editing,
    rewards: editing.rewards.map((reward) => {
      const plan = plans.data?.find((plan) => String(plan.id) === String(reward.plan_id))
      return reward.kind === 'subscription' && editing.state === 'draft' && !reward.plan_snapshot
        ? { ...reward, plan_snapshot: plan }
        : reward
    }),
  }
  const preview = previewBatch ? batchReservePreview(previewBatch) : undefined
  const editField = (field: keyof BoxBatch, value: string | boolean) =>
    setEditing((current) => current && { ...current, [field]: value })
  const editReward = (index: number, field: keyof BoxBatchReward, value: string) =>
    setEditing(
      (current) =>
        current && {
          ...current,
          rewards: current.rewards.map((reward, i) =>
            i === index
              ? {
                  ...reward,
                  [field]: value,
                  ...(field === 'kind' || field === 'plan_id'
                    ? {
                        plan_snapshot: undefined,
                        ...(value === 'credits' ? { plan_id: 0 } : { amount_micro: 0 }),
                      }
                    : {}),
                }
              : reward,
          ),
        },
    )
  const numeric = (field: 'ancillary_cost_ppm' | 'contribution_share_ppm', label: string) => (
    <label className="field" htmlFor={`batch-${field}`}>
      <span>{t(label)}</span>
      <input
        id={`batch-${field}`}
        inputMode="numeric"
        dir="ltr"
        pattern="[0-9]+"
        required
        disabled={pending || immutable}
        value={String(editing?.[field] ?? 0)}
        onChange={(event) => editField(field, event.target.value)}
      />
    </label>
  )
  return (
    <section className="section box-batch-admin">
      <div className="page-header">
        <h2>{t('批次与准备金')}</h2>
        <Button
          disabled={pending}
          onClick={() => {
            save.reset()
            changeState.reset()
            setError(null)
            setSavedConfiguration('')
            setEditing(newBoxBatch())
          }}
        >
          {t('新建回馈批次')}
        </Button>
      </div>
      <p className="muted">
        {t(
          '草稿可编辑；发布冻结奖励规格和数量，并从管理员钱包实扣全额准备金。不会自动启用示例奖池。',
        )}
      </p>
      <ErrorMessage error={error ?? query.error ?? save.error ?? changeState.error} />
      {query.isError && (
        <Button variant="quiet" onClick={() => void query.refetch()}>
          {t('重试')}
        </Button>
      )}
      {query.isPending && <Loading />}
      <DataTable
        rows={query.data ?? []}
        rowKey={(row) => String(row.id)}
        empty="暂无回馈批次"
        columns={[
          {
            label: '批次',
            render: (row) => (
              <>
                <bdi>#{String(row.id)}</bdi> {row.name}
              </>
            ),
          },
          {
            label: '类型',
            render: (row) => t(row.purpose === 'consumption' ? '消费回馈' : '额度回馈'),
          },
          { label: '状态', render: (row) => t(batchStateLabel(row.state)) },
          {
            label: '剩余份数',
            render: (row) => `${String(row.remaining_count)} / ${String(row.total_count)}`,
            numeric: true,
          },
          {
            label: '已锁准备金',
            render: (row) => credits(row.remaining_budget_micro),
            numeric: true,
          },
          {
            label: '操作',
            render: (row) => (
              <div className="box-prop-actions">
                <Button
                  variant="quiet"
                  disabled={pending}
                  onClick={() => {
                    save.reset()
                    changeState.reset()
                    setError(null)
                    setSavedConfiguration(poolConfigJSON(row))
                    setEditing(row)
                  }}
                >
                  {t(row.state === 'draft' ? '编辑' : '查看冻结规格')}
                </Button>
                <Button variant="quiet" onClick={() => setStatsID(String(row.id))}>
                  {t('查看统计')}
                </Button>
              </div>
            ),
          },
        ]}
      />
      {editing && (
        <BatchForm
          key={`${editing.id}:${editing.revision}`}
          batch={editing}
          immutable={immutable}
          pending={pending}
          dirty={dirty}
          preview={preview}
          plans={plans.data ?? []}
          plansError={plans.error}
          onEdit={editField}
          onRewardEdit={editReward}
          onAdd={() =>
            setEditing(
              (current) =>
                current && { ...current, rewards: [...current.rewards, newBatchReward()] },
            )
          }
          onRemove={(index) =>
            setEditing(
              (current) =>
                current && { ...current, rewards: current.rewards.filter((_, i) => i !== index) },
            )
          }
          onSave={(amounts) => {
            const draft = { ...editing, ...amounts }
            try {
              validateBatchDraft(draft)
              setError(null)
              save.mutate(draft)
            } catch (failure) {
              setError(failure instanceof Error ? failure : new Error('配置无效'))
            }
          }}
          onClose={() => setEditing(null)}
          onReview={(action) => void reviewState(editing, action)}
          rateFields={
            <>
              {numeric('ancillary_cost_ppm', '附加成本比例（ppm，10000 = 1%）')}
              {numeric('contribution_share_ppm', '正贡献活动预算比例（ppm，上限 100000 = 10%）')}
            </>
          }
        />
      )}
      {statsID && (
        <section
          className="section box-batch-statistics"
          aria-labelledby="batch-statistics-heading"
        >
          <div className="page-header">
            <h3 id="batch-statistics-heading">
              {t('批次统计')} <bdi>#{statsID}</bdi>
            </h3>
            <Button
              variant="quiet"
              disabled={stats.isFetching}
              onClick={() => void stats.refetch()}
            >
              {t('刷新统计')}
            </Button>
          </div>
          <ErrorMessage error={stats.error} />
          {stats.isPending && <Loading />}
          {stats.data && (
            <dl className="box-statistics-grid">
              {(
                [
                  ['已揭晓份数', String(stats.data.draw_count)],
                  ['确定额度已发放', credits(stats.data.base_credits_micro)],
                  ['随机额度已发放', credits(stats.data.reward_credits_micro)],
                  ['套餐卡已发放', String(stats.data.subscription_awarded_count)],
                  ['套餐已激活', String(stats.data.subscription_activated_count)],
                  ['真实 API 已使用', credits(stats.data.api_used_micro)],
                  ['初始锁定准备金', credits(stats.data.reserved_micro)],
                  ['已兑现准备金', credits(stats.data.spent_micro)],
                  ['剩余准备金', credits(stats.data.remaining_micro)],
                ] as const
              ).map(([label, value]) => (
                <div key={label}>
                  <dt>{t(label)}</dt>
                  <dd>{value}</dd>
                </div>
              ))}
            </dl>
          )}
          <p className="muted">{t('准备金按承诺全额核算；未使用和到期不能直接视为活动利润。')}</p>
        </section>
      )}
    </section>
  )
}

import type { ReactNode } from 'react'
import type { Schema } from '../../lib/types'

function BatchForm(props: {
  batch: BoxBatch
  immutable: boolean
  pending: boolean
  dirty: boolean
  preview: ReturnType<typeof batchReservePreview>
  plans: Schema['Plan'][]
  plansError: Error | null
  onEdit: (field: keyof BoxBatch, value: string | boolean) => void
  onRewardEdit: (index: number, field: keyof BoxBatchReward, value: string) => void
  onAdd: () => void
  onRemove: (index: number) => void
  onSave: (
    amounts: Pick<BoxBatch, 'price_micro' | 'base_credits_micro' | 'budget_micro' | 'rewards'>,
  ) => void
  onClose: () => void
  onReview: (action: 'publish' | 'pause') => void
  rateFields: ReactNode
}) {
  const { t } = useTranslation()
  const { batch, immutable, pending, preview } = props
  const [formError, setFormError] = useState<Error | null>(null)
  const [amounts, setAmounts] = useState(() => ({
    price: decimalCredits(batch.price_micro),
    base: decimalCredits(batch.base_credits_micro),
    budget: decimalCredits(batch.budget_micro),
  }))
  const [rewardAmounts, setRewardAmounts] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      batch.rewards.map((reward) => [reward.id, decimalCredits(reward.amount_micro)]),
    ),
  )
  const field = (name: 'price' | 'base' | 'budget', label: string) => (
    <label className="field" htmlFor={`batch-${name}`}>
      <span>{t(label)}</span>
      <input
        id={`batch-${name}`}
        dir="ltr"
        inputMode="decimal"
        required
        pattern="[0-9]+([.][0-9]{1,6})?"
        disabled={pending || immutable}
        value={amounts[name]}
        onChange={(event) => {
          const value = event.target.value
          setAmounts((current) => ({ ...current, [name]: value }))
          try {
            props.onEdit(
              name === 'price'
                ? 'price_micro'
                : name === 'base'
                  ? 'base_credits_micro'
                  : 'budget_micro',
              nonnegativeMicroCredits(value).toString(),
            )
          } catch {
            /* Keep partial decimal text until submit validates it. */
          }
        }}
      />
    </label>
  )
  return (
    <form
      className="form-panel box-batch-form"
      onSubmit={(event) => {
        event.preventDefault()
        if (pending || immutable) return
        try {
          const rewards = batch.rewards.map((reward) => ({
            ...reward,
            amount_micro:
              reward.kind === 'credits'
                ? nonnegativeMicroCredits(rewardAmounts[reward.id] ?? '0')
                : 0,
          }))
          setFormError(null)
          props.onSave({
            price_micro: nonnegativeMicroCredits(amounts.price),
            base_credits_micro: nonnegativeMicroCredits(amounts.base),
            budget_micro: nonnegativeMicroCredits(amounts.budget),
            rewards,
          })
        } catch (failure) {
          setFormError(failure instanceof Error ? failure : new Error('配置无效'))
        }
      }}
    >
      <h3 className="full-width">
        {t(immutable ? '已发布冻结规格' : '编辑批次草稿')}{' '}
        {boxInteger(batch.id) > 0n && (
          <bdi>
            #{String(batch.id)} · r{String(batch.revision)}
          </bdi>
        )}
      </h3>
      <ErrorMessage error={formError} />
      <ErrorMessage error={props.plansError} />
      <label className="field" htmlFor="batch-name">
        <span>{t('批次名称')}</span>
        <input
          id="batch-name"
          required
          maxLength={200}
          disabled={pending || immutable}
          value={batch.name}
          onChange={(event) => props.onEdit('name', event.target.value)}
        />
      </label>
      <label className="field" htmlFor="batch-purpose">
        <span>{t('回馈类型')}</span>
        <select
          id="batch-purpose"
          disabled={pending || immutable}
          value={batch.purpose}
          onChange={(event) => props.onEdit('purpose', event.target.value)}
        >
          <option value="consumption">{t('消费回馈')}</option>
          <option value="credits">{t('额度回馈')}</option>
        </select>
      </label>
      {field('price', '钱包扣款（credits）')}
      {field('base', '确定消费额度（credits）')}
      {field('budget', '准备金预算（credits）')}
      {props.rateFields}
      <p className="muted full-width">
        {t(
          'credits 是消费额度，不是人民币。1 credit = 1,000,000 micro-credits。免费回馈的价格和基础额度须为 0。',
        )}
      </p>
      <div className="full-width">
        <h4>{t('固定数量奖池')}</h4>
        {batch.rewards.map((reward, index) => (
          <fieldset key={reward.id} className="pool-reward-row" disabled={pending || immutable}>
            <legend>
              {t('奖励')} {index + 1} ·{' '}
              {preview ? (weightPercentage(reward.quantity, preview.count) ?? '—') : '—'}
            </legend>
            <div className="pool-reward-grid">
              <label className="field" htmlFor={`batch-reward-${reward.id}-title`}>
                <span>{t('展示名称')}</span>
                <input
                  id={`batch-reward-${reward.id}-title`}
                  required
                  maxLength={200}
                  value={reward.title}
                  onChange={(event) => props.onRewardEdit(index, 'title', event.target.value)}
                />
              </label>
              <label className="field" htmlFor={`batch-reward-${reward.id}-kind`}>
                <span>{t('奖励类型')}</span>
                <select
                  id={`batch-reward-${reward.id}-kind`}
                  value={reward.kind}
                  onChange={(event) => props.onRewardEdit(index, 'kind', event.target.value)}
                >
                  <option value="credits">{t('消费额度')}</option>
                  <option value="subscription">{t('固定额度套餐')}</option>
                </select>
              </label>
              <label className="field" htmlFor={`batch-reward-${reward.id}-quantity`}>
                <span>{t('固定发放数量')}</span>
                <input
                  id={`batch-reward-${reward.id}-quantity`}
                  dir="ltr"
                  inputMode="numeric"
                  pattern="[1-9][0-9]*"
                  required
                  value={String(reward.quantity)}
                  onChange={(event) => props.onRewardEdit(index, 'quantity', event.target.value)}
                />
              </label>
              {reward.kind === 'credits' ? (
                <label className="field" htmlFor={`batch-reward-${reward.id}-amount`}>
                  <span>{t('奖励额度（credits）')}</span>
                  <input
                    id={`batch-reward-${reward.id}-amount`}
                    dir="ltr"
                    inputMode="decimal"
                    pattern="[0-9]+([.][0-9]{1,6})?"
                    required
                    value={rewardAmounts[reward.id] ?? decimalCredits(reward.amount_micro)}
                    onChange={(event) => {
                      const value = event.target.value
                      setRewardAmounts((current) => ({ ...current, [reward.id]: value }))
                      try {
                        props.onRewardEdit(
                          index,
                          'amount_micro',
                          nonnegativeMicroCredits(value).toString(),
                        )
                      } catch {
                        /* Validate the partial decimal value on submit. */
                      }
                    }}
                  />
                </label>
              ) : (
                <label className="field" htmlFor={`batch-reward-${reward.id}-plan`}>
                  <span>{t('固定额度套餐')}</span>
                  <select
                    id={`batch-reward-${reward.id}-plan`}
                    value={String(reward.plan_id)}
                    required
                    onChange={(event) => props.onRewardEdit(index, 'plan_id', event.target.value)}
                  >
                    <option value="0">{t('请选择有效的固定额度套餐')}</option>
                    {props.plans
                      .filter(
                        (plan) =>
                          plan.policy_version === 'standard_v2' &&
                          (plan.enabled || String(plan.id) === String(reward.plan_id)),
                      )
                      .map((plan) => (
                        <option key={String(plan.id)} value={String(plan.id)}>
                          {plan.name} · {credits(plan.credits)} · #{String(plan.id)}
                        </option>
                      ))}
                  </select>
                </label>
              )}
              {reward.kind === 'subscription' && reward.plan_snapshot && (
                <BoxPlanSpecification snapshot={reward.plan_snapshot} />
              )}
            </div>
            {!immutable && (
              <Button
                type="button"
                variant="quiet"
                disabled={pending || batch.rewards.length <= 1}
                onClick={() => props.onRemove(index)}
              >
                {t('移除此奖励')}
              </Button>
            )}
          </fieldset>
        ))}
        {!immutable && (
          <Button
            type="button"
            variant="secondary"
            disabled={pending || batch.rewards.length >= 100}
            onClick={props.onAdd}
          >
            {t('添加奖励')}
          </Button>
        )}
      </div>
      <div className="box-reserve-preview full-width">
        <h4>{t('发布前准备金预览')}</h4>
        <dl className="box-limits">
          <div>
            <dt>{t('总份数')}</dt>
            <dd>{preview ? String(preview.count) : '—'}</dd>
          </div>
          <div>
            <dt>{t('全部权益兑现的准备金')}</dt>
            <dd>{preview ? credits(preview.required) : '—'}</dd>
          </div>
          <div>
            <dt>{t('服务端核定准备金')}</dt>
            <dd>{credits(batch.required_budget_micro)}</dd>
          </div>
        </dl>
        <p className="muted">
          {t(
            '准备金按基础额度与奖品全额使用核算。附加成本用于计算消费净贡献；保存草稿时冻结套餐规格，发布沿用已保存规格并由服务端重新校验。',
          )}
        </p>
        {preview && preview.required > BigInt(batch.budget_micro || 0) && (
          <p className="notice">{t('准备金预算不足，补足后才能发布。')}</p>
        )}
      </div>
      <label className="checkbox-field full-width">
        <input
          type="checkbox"
          disabled={pending || immutable}
          checked={batch.costs_confirmed}
          onChange={(event) => props.onEdit('costs_confirmed', event.target.checked)}
        />
        {t('我已核对真实成本、其他活动叠加和全额履约预算。')}
      </label>
      {!immutable && (
        <Button type="submit" loading={pending} disabled={pending}>
          {t('保存草稿')}
        </Button>
      )}
      {batch.state === 'draft' && boxInteger(batch.id) > 0n && (
        <Button
          type="button"
          disabled={
            pending ||
            props.dirty ||
            !batch.costs_confirmed ||
            BigInt(batch.required_budget_micro) <= 0n
          }
          onClick={() => props.onReview('publish')}
        >
          {t('发布并锁定准备金')}
        </Button>
      )}
      {!immutable && props.dirty && (
        <p className="muted full-width">{t('有未保存的修改，请先保存草稿再发布。')}</p>
      )}
      {batch.state === 'published' && (
        <Button
          type="button"
          variant="quiet"
          disabled={pending}
          onClick={() => props.onReview('pause')}
        >
          {t('停止新发放')}
        </Button>
      )}
      <Button type="button" variant="quiet" disabled={pending} onClick={props.onClose}>
        {t('关闭')}
      </Button>
      {immutable && (
        <p className="muted full-width">
          {t('发布后规格不可修改；修改规则请新建批次。停售不影响既有领取资格和已获得权益。')}
        </p>
      )}
      {batch.published_at && (
        <p className="muted full-width">
          {t('发布时间')} · {date(batch.published_at)}
        </p>
      )}
    </form>
  )
}
