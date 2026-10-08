import { useTranslation } from '../../lib/i18n'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { credits } from '../../lib/format'
import { decimalCredits, errorFrom, nonnegativeMicroCredits, positiveID } from './amounts'
import { subscriptionPolicy } from '../../lib/subscription-policy'
import { Button, ErrorMessage, Field, Loading } from '../../components/ui'
import { DataTable } from '../../components/data-table'
import { WalletConversionReviewPanel } from './wallet-conversion-review-panel'

export function SubscriptionRedesignAdmin(props: { plans: Schema['Plan'][] }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [error, setError] = useState<Error | null>(null)
  const [editing, setEditing] = useState<Schema['ConversionRule'] | 'new' | null>(null)
  const [card, setCard] = useState<Schema['ResetCardRule'] | 'new' | null>(null)
  const [reviewSubscription, setReviewSubscription] = useState<Schema['Subscription']['id'] | null>(
    null,
  )
  const rules = useQuery({
    queryKey: ['subscription-redesign-rules'],
    queryFn: ({ signal }) =>
      api.GET('/api/subscription/admin/redesign-rules', { signal }).then(unwrap),
    retry: false,
  })
  const preview = useMutation({
    mutationFn: () => api.POST('/api/subscription/admin/redesign-preview').then(unwrap),
  })
  const save = useMutation({
    mutationFn: (body: Schema['RedesignRules']) =>
      api.PUT('/api/subscription/admin/redesign-rules', { body }).then(unwrap),
    onSuccess: () => {
      setEditing(null)
      setCard(null)
      void client.invalidateQueries({ queryKey: ['subscription-redesign-rules'] })
    },
  })
  const conversion = editing && editing !== 'new' ? editing : undefined
  const cardRule = card && card !== 'new' ? card : undefined
  return (
    <section className="section">
      <h2>{t('套餐转换与历史换卡配置')}</h2>
      <p className="muted">
        {t(
          '仅根管理员可配置。先核对每个老版本的额度、原付费构成、实际履约支出和增量补偿，再标记审核并启用。未审核规则不向用户报价。',
        )}
      </p>
      <ErrorMessage error={error ?? rules.error ?? save.error ?? preview.error} />
      {rules.isPending && <Loading />}
      <div className="row-actions">
        <Button variant="quiet" disabled={preview.isPending} onClick={() => preview.mutate()}>
          {preview.isPending ? t('盘点中…') : t('查看存量与成本预览')}
        </Button>
        <Button
          variant="quiet"
          disabled={save.isPending}
          onClick={() => {
            setError(null)
            save.reset()
            setEditing('new')
            setCard(null)
          }}
        >
          {t('新增老套餐转换比例')}
        </Button>
        <Button
          variant="quiet"
          disabled={save.isPending}
          onClick={() => {
            setError(null)
            save.reset()
            setCard('new')
            setEditing(null)
          }}
        >
          {t('新增历史次数换卡档位')}
        </Button>
      </div>
      <form
        className="row-actions"
        onSubmit={(event) => {
          event.preventDefault()
          try {
            setReviewSubscription(
              positiveID(
                String(new FormData(event.currentTarget).get('review-subscription') ?? ''),
              ),
            )
            setError(null)
          } catch (cause) {
            setError(errorFrom(cause))
          }
        }}
      >
        <Field name="review-subscription" label="订阅" required />
        <Button type="submit">{t('核定分段转换')}</Button>
      </form>
      {preview.data && (
        <>
          <p>
            {t('有效老套餐')} {String(preview.data.active_legacy_count)} {t('份；已有刷新次数')}{' '}
            {String(preview.data.available_reset_count)}{' '}
            {t('次。下表只展示本次盘点返回的候选，不代表所有账户都已核对。')}
          </p>
          <DataTable
            rows={preview.data.candidates}
            rowKey={(row) => row.subscription_id}
            columns={[
              { label: '订阅', render: (row) => String(row.subscription_id) },
              { label: '估值依据', render: (row) => row.basis_key },
              { label: '原总额度', render: (row) => credits(row.source_total) },
              {
                label: '剩余 / 可兑余额',
                render: (row) => `${credits(row.source_credits)} / ${credits(row.target_credits)}`,
              },
              { label: '核对状态', render: (row) => row.review_reason || row.state },
              {
                label: '操作',
                render: (row) => (
                  <Button
                    variant="quiet"
                    onClick={() => setReviewSubscription(row.subscription_id)}
                  >
                    {t('核定分段转换')}
                  </Button>
                ),
              },
            ]}
          />
        </>
      )}
      {reviewSubscription !== null && (
        <WalletConversionReviewPanel
          key={String(reviewSubscription)}
          subscriptionID={reviewSubscription}
          onClose={() => setReviewSubscription(null)}
        />
      )}
      <h3>{t('老套餐转余额比例')}</h3>
      <DataTable
        rows={rules.data?.conversion_rules ?? []}
        rowKey={(row) => row.id}
        columns={[
          {
            label: '老套餐',
            render: (row) =>
              props.plans.find((plan) => String(plan.id) === String(row.plan_id))?.name ??
              String(row.plan_id),
          },
          {
            label: '整包老额度 → 钱包',
            render: (row) => `${credits(row.source_credits)} → ${credits(row.wallet_credits)}`,
          },
          {
            label: '付费 / 刷新后付费',
            render: (row) =>
              `${credits(row.paid_wallet_credits)} / ${credits(row.refreshed_paid_wallet_credits ?? undefined)}`,
          },
          {
            label: '状态',
            render: (row) =>
              String(row.reviewed ? '已审核' : '待审核') +
              ' · ' +
              String(row.enabled ? '启用' : '停用'),
          },
          {
            label: '操作',
            render: (row) => (
              <Button
                variant="quiet"
                onClick={() => {
                  setEditing(row)
                  setCard(null)
                  save.reset()
                }}
              >
                {t('编辑比例')}
              </Button>
            ),
          },
        ]}
      />
      {editing && (
        <form
          key={String(conversion?.id ?? 'new-conversion')}
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              const form = new FormData(event.currentTarget)
              const text = (name: string) => String(form.get(name) ?? '').trim()
              const amount = (name: string) => nonnegativeMicroCredits(text(name))
              const reviewed = form.has('conversion-reviewed')
              const enabled = form.has('conversion-enabled')
              if (enabled && !reviewed) throw new Error('启用前必须完成审核')
              save.mutate({
                conversion_rules: [
                  {
                    ...conversion,
                    id: conversion?.id ?? 0,
                    revision: conversion?.revision ?? 0,
                    plan_id: positiveID(text('conversion-plan')),
                    basis_key: text('conversion-basis'),
                    source_credits: amount('conversion-source'),
                    wallet_credits: amount('conversion-wallet'),
                    paid_wallet_credits: amount('conversion-paid'),
                    recognized_revenue_credits: text('conversion-revenue')
                      ? amount('conversion-revenue')
                      : undefined,
                    refreshed_paid_wallet_credits: text('conversion-refreshed-paid')
                      ? amount('conversion-refreshed-paid')
                      : undefined,
                    reviewed,
                    enabled,
                    note: text('conversion-note'),
                  },
                ],
                card_rules: [],
              })
            } catch (cause) {
              setError(errorFrom(cause))
            }
          }}
        >
          <label className="field" htmlFor="conversion-plan">
            <span>{t('老套餐档位')}</span>
            <select
              id="conversion-plan"
              name="conversion-plan"
              defaultValue={String(conversion?.plan_id ?? '')}
              disabled={!!conversion}
              required
            >
              <option value="">{t('选择原规则套餐')}</option>
              {props.plans
                .filter((plan) => subscriptionPolicy(plan) === 'legacy')
                .map((plan) => (
                  <option key={String(plan.id)} value={String(plan.id)}>
                    {plan.name} · {String(plan.id)}
                  </option>
                ))}
            </select>
          </label>
          {conversion && (
            <input type="hidden" name="conversion-plan" value={String(conversion.plan_id)} />
          )}
          <Field
            name="conversion-basis"
            label="原版本估值依据（盘点返回值）"
            required
            defaultValue={conversion?.basis_key}
          />
          <Field
            name="conversion-source"
            label="整包老额度 credits"
            required
            defaultValue={decimalCredits(conversion?.source_credits ?? 0)}
          />
          <Field
            name="conversion-wallet"
            label="整包可兑余额 credits"
            required
            defaultValue={decimalCredits(conversion?.wallet_credits ?? 0)}
          />
          <Field
            name="conversion-paid"
            label="其中原付费 credits"
            required
            defaultValue={decimalCredits(conversion?.paid_wallet_credits ?? 0)}
          />
          <Field
            name="conversion-refreshed-paid"
            label="已刷新套餐原付费 credits（留空须核对）"
            defaultValue={
              conversion?.refreshed_paid_wallet_credits == null
                ? ''
                : decimalCredits(conversion.refreshed_paid_wallet_credits)
            }
          />
          <Field
            name="conversion-revenue"
            label="原实付收入等值 credits（未知留空）"
            defaultValue={
              conversion?.recognized_revenue_credits == null
                ? ''
                : decimalCredits(conversion.recognized_revenue_credits)
            }
          />
          <Field
            name="conversion-note"
            label="核对依据与说明"
            defaultValue={conversion?.note}
            maxLength={2000}
          />
          <label className="checkbox-field">
            <input
              type="checkbox"
              name="conversion-reviewed"
              defaultChecked={conversion?.reviewed ?? false}
            />
            {t('已核对权益与资金来源')}
          </label>
          <label className="checkbox-field">
            <input
              type="checkbox"
              name="conversion-enabled"
              defaultChecked={conversion?.enabled ?? false}
            />
            {t('启用此比例')}
          </label>
          <Button type="submit" disabled={save.isPending}>
            {t('保存转换比例')}
          </Button>
          <Button variant="quiet" disabled={save.isPending} onClick={() => setEditing(null)}>
            {t('取消')}
          </Button>
        </form>
      )}
      <h3>{t('历史次数换卡与预算')}</h3>
      <DataTable
        rows={rules.data?.card_rules ?? []}
        rowKey={(row) => row.id}
        columns={[
          { label: '档位', render: (row) => row.name },
          { label: '每张额度', render: (row) => credits(row.credits) },
          {
            label: '实际履约 / 原权益成本',
            render: (row) => `${credits(row.cost_per_card)} / ${credits(row.baseline_cost)}`,
          },
          {
            label: '全额预算 / 已预留',
            render: (row) => `${credits(row.budget_total)} / ${credits(row.budget_reserved)}`,
          },
          {
            label: '增量预算 / 已预留',
            render: (row) =>
              `${credits(row.incremental_budget_total)} / ${credits(row.incremental_reserved)}`,
          },
          {
            label: '状态',
            render: (row) =>
              String(row.reviewed ? '已审核' : '待审核') +
              ' · ' +
              String(row.enabled ? '启用' : '停用'),
          },
          {
            label: '操作',
            render: (row) => (
              <Button
                variant="quiet"
                onClick={() => {
                  setCard(row)
                  setEditing(null)
                  save.reset()
                }}
              >
                {t('编辑换卡档位')}
              </Button>
            ),
          },
        ]}
      />
      {card && (
        <form
          key={String(cardRule?.id ?? 'new-card')}
          className="form-panel"
          onSubmit={(event) => {
            event.preventDefault()
            setError(null)
            try {
              const form = new FormData(event.currentTarget)
              const text = (name: string) => String(form.get(name) ?? '').trim()
              const amount = (name: string) => nonnegativeMicroCredits(text(name))
              const reviewed = form.has('card-reviewed')
              const enabled = form.has('card-enabled')
              if (enabled && !reviewed) throw new Error('启用前必须完成审核')
              save.mutate({
                conversion_rules: [],
                card_rules: [
                  {
                    ...cardRule,
                    id: cardRule?.id ?? 0,
                    revision: cardRule?.revision ?? 0,
                    name: text('card-name'),
                    reference_plan_id: positiveID(text('card-reference')),
                    card_plan_id: positiveID(text('card-plan')),
                    credits: amount('card-credits'),
                    cost_per_card: amount('card-cost'),
                    baseline_cost: amount('card-baseline'),
                    budget_total: amount('card-budget'),
                    incremental_budget_total: amount('card-incremental-budget'),
                    budget_reserved: cardRule?.budget_reserved ?? 0,
                    incremental_reserved: cardRule?.incremental_reserved ?? 0,
                    enabled,
                    reviewed,
                    note: text('card-note'),
                  },
                ],
              })
            } catch (cause) {
              setError(errorFrom(cause))
            }
          }}
        >
          <Field name="card-name" label="换卡档位名称" required defaultValue={cardRule?.name} />
          <label className="field" htmlFor="card-reference">
            <span>{t('原资格参考套餐')}</span>
            <select
              id="card-reference"
              name="card-reference"
              defaultValue={String(cardRule?.reference_plan_id ?? '')}
              required
            >
              <option value="">{t('选择老套餐')}</option>
              {props.plans
                .filter((plan) => subscriptionPolicy(plan) === 'legacy')
                .map((plan) => (
                  <option key={String(plan.id)} value={String(plan.id)}>
                    {plan.name}
                  </option>
                ))}
            </select>
          </label>
          <label className="field" htmlFor="card-plan">
            <span>{t('兑换卡新版规格')}</span>
            <select
              id="card-plan"
              name="card-plan"
              defaultValue={String(cardRule?.card_plan_id ?? '')}
              required
            >
              <option value="">{t('选择固定额度新版')}</option>
              {props.plans
                .filter((plan) => subscriptionPolicy(plan) === 'standard_v2')
                .map((plan) => (
                  <option key={String(plan.id)} value={String(plan.id)}>
                    {plan.name}
                  </option>
                ))}
            </select>
          </label>
          <Field
            name="card-credits"
            label="每张兑换卡 credits"
            required
            defaultValue={decimalCredits(cardRule?.credits ?? 0)}
          />
          <Field
            name="card-cost"
            label="每张用满实际履约成本 credits"
            required
            defaultValue={decimalCredits(cardRule?.cost_per_card ?? 0)}
          />
          <Field
            name="card-baseline"
            label="原刷新等值履约成本 credits"
            required
            defaultValue={decimalCredits(cardRule?.baseline_cost ?? 0)}
          />
          <Field
            name="card-budget"
            label="实际履约总预算 credits"
            required
            defaultValue={decimalCredits(cardRule?.budget_total ?? 0)}
          />
          <Field
            name="card-incremental-budget"
            label="增量补偿总预算 credits"
            required
            defaultValue={decimalCredits(cardRule?.incremental_budget_total ?? 0)}
          />
          <Field
            name="card-note"
            label="换卡核对依据与说明"
            defaultValue={cardRule?.note}
            maxLength={2000}
          />
          <label className="checkbox-field">
            <input
              type="checkbox"
              name="card-reviewed"
              defaultChecked={cardRule?.reviewed ?? false}
            />
            {t('已核对资格、价值与两笔成本预算')}
          </label>
          <label className="checkbox-field">
            <input
              type="checkbox"
              name="card-enabled"
              defaultChecked={cardRule?.enabled ?? false}
            />
            {t('启用此换卡档位')}
          </label>
          <Button type="submit" disabled={save.isPending}>
            {t('保存换卡档位')}
          </Button>
          <Button variant="quiet" disabled={save.isPending} onClick={() => setCard(null)}>
            {t('取消')}
          </Button>
        </form>
      )}
      {save.isSuccess && (
        <p role="status">{t('配置已保存，用户已确认的权益继续按原冻结版本履行。')}</p>
      )}
    </section>
  )
}
