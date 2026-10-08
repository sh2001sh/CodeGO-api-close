import { useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import type { Schema } from '../../lib/types'
import { useTranslation } from '../../lib/i18n'
import { credits, date } from '../../lib/format'
import { Button, ErrorMessage, Field, Loading } from '../../components/ui'
import { DataTable } from '../../components/data-table'
import { decimalCredits, errorFrom, nonnegativeMicroCredits, positiveID } from './amounts'

type Segment = Schema['WalletConversionSegment']

export function WalletConversionReviewPanel(props: {
  subscriptionID: Schema['WalletConversionReview']['subscription_id']
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [error, setError] = useState<Error | null>(null)
  const facts = useQuery({
    queryKey: ['wallet-conversion-review', props.subscriptionID],
    queryFn: ({ signal }) =>
      api
        .GET('/api/subscription/admin/wallet-conversion-review/{id}', {
          params: { path: { id: props.subscriptionID } },
          signal,
        })
        .then(unwrap),
    retry: false,
  })
  const save = useMutation({
    mutationFn: (body: Schema['WalletConversionReview']) =>
      api
        .PUT('/api/subscription/admin/wallet-conversion-review/{id}', {
          params: { path: { id: props.subscriptionID } },
          body,
        })
        .then(unwrap),
    onSuccess: () => {
      void facts.refetch()
    },
  })
  return (
    <section className="section">
      <h3>{t('核定分段转换')}</h3>
      <p className="muted">
        {t(
          '按原订单与赠送来源逐段核对。当前额度和未发周期承诺必须全部覆盖；只保存核定报价，不会替用户转换。消费、刷新或退款后需要重新核定。',
        )}
      </p>
      <ErrorMessage error={error ?? facts.error ?? save.error} />
      {facts.isPending && <Loading />}
      {facts.data && (
        <>
          <dl className="metrics">
            <div>
              <dt>{t('订阅')}</dt>
              <dd>{String(props.subscriptionID)}</dd>
            </div>
            <div>
              <dt>{t('当前可用老额度')}</dt>
              <dd>{credits(facts.data.current_credits)}</dd>
            </div>
            <div>
              <dt>{t('未发放的周期承诺')}</dt>
              <dd>{credits(facts.data.future_credits)}</dd>
            </div>
            <div>
              <dt>{t('原套餐到期')}</dt>
              <dd>{date(facts.data.expires_at)}</dd>
            </div>
          </dl>
          <DataTable
            rows={facts.data.sources}
            rowKey={(row) => row.order_id}
            columns={[
              { label: '订单', render: (row) => String(row.order_id) },
              { label: '状态', render: (row) => row.state },
              { label: '原总额度', render: (row) => credits(row.credits) },
              {
                label: '原实付收入等值 credits（未知留空）',
                render: (row) => credits(row.revenue_credits ?? undefined),
              },
              { label: '已转换本金', render: (row) => credits(row.previously_paid) },
            ]}
          />
          <ReviewForm
            key={facts.data.fact_hash + ':' + String(facts.data.review?.revision ?? 0)}
            facts={facts.data}
            pending={save.isPending}
            onError={setError}
            onSave={(body) => {
              setError(null)
              save.mutate(body)
            }}
          />
        </>
      )}
      {save.isSuccess && <p role="status">{t('分段核定已保存，用户仍须预览报价并同意转换。')}</p>}
      <Button variant="quiet" disabled={save.isPending} onClick={props.onClose}>
        {t('关闭')}
      </Button>
    </section>
  )
}

function ReviewForm(props: {
  facts: Schema['WalletConversionReviewEvidence']
  pending: boolean
  onError: (error: Error | null) => void
  onSave: (body: Schema['WalletConversionReview']) => void
}) {
  const { t } = useTranslation()
  const prior = props.facts.review
  const [segments, setSegments] = useState<Array<{ key: string; value?: Segment }>>(
    prior?.segments.map((value, i) => ({ key: String(i), value })) ?? [
      { key: crypto.randomUUID() },
    ],
  )
  return (
    <form
      className="form-panel"
      onSubmit={(event) => {
        event.preventDefault()
        props.onError(null)
        try {
          const form = new FormData(event.currentTarget)
          const reviewed = form.has('review-checked')
          const enabled = form.has('review-enabled')
          if (enabled && !reviewed) throw new Error(t('启用前必须完成审核'))
          if (prior && !enabled) {
            props.onSave({
              ...prior,
              enabled: false,
              reviewed,
              note: String(form.get('review-note') ?? '').trim(),
            })
            return
          }
          const values = segments.map(({ key }) => {
            const text = (name: string) => String(form.get(`${key}-${name}`) ?? '').trim()
            const amount = (name: string) => nonnegativeMicroCredits(text(name))
            const source = amount('source')
            const current = amount('current')
            const future = amount('future')
            const wallet = amount('wallet')
            const paid = amount('paid')
            if (
              source <= 0n ||
              wallet <= 0n ||
              current + future > source ||
              current + future === 0n ||
              paid > wallet
            )
              throw new Error(t('分段额度或付费构成无效'))
            return {
              name: text('name'),
              original_order_id: text('order') ? BigInt(positiveID(text('order'))) : 0n,
              source_total: source,
              current_credits: current,
              future_credits: future,
              wallet_credits: wallet,
              paid_wallet_credits: paid,
              target_credits: 0n,
              paid_credits: 0n,
              revenue_multiplier_ppm: 0n,
            }
          })
          if (
            values.reduce((sum, p) => sum + p.current_credits, 0n) !==
              BigInt(props.facts.current_credits) ||
            values.reduce((sum, p) => sum + p.future_credits, 0n) !==
              BigInt(props.facts.future_credits)
          )
            throw new Error(t('分段合计必须覆盖全部当前额度及未来周期承诺'))
          props.onSave({
            ...prior,
            id: prior?.id ?? 0n,
            subscription_id: props.facts.subscription_id,
            fact_hash: props.facts.fact_hash,
            revision: prior?.revision ?? 0n,
            note: String(form.get('review-note') ?? '').trim(),
            segments: values,
            enabled,
            reviewed,
            reviewer_id: 0n,
            reviewed_at: new Date().toISOString(),
          })
        } catch (cause) {
          props.onError(errorFrom(cause))
        }
      }}
    >
      {segments.map(({ key, value }, index) => (
        <fieldset key={key} disabled={props.pending}>
          <legend>
            {t('权益分段')} {index + 1}
          </legend>
          <Field
            name={`${key}-name`}
            label="来源说明"
            required
            maxLength={200}
            defaultValue={value?.name}
          />
          <label className="field" htmlFor={`${key}-order`}>
            <span>{t('原付费订单（赠送可不选）')}</span>
            <select
              id={`${key}-order`}
              name={`${key}-order`}
              defaultValue={String(value?.original_order_id || '')}
            >
              <option value="">{t('赠送或独立刷新权益')}</option>
              {props.facts.sources.map((source) => (
                <option key={String(source.order_id)} value={String(source.order_id)}>
                  {String(source.order_id)} · {t(source.state)} · {credits(source.credits)}
                </option>
              ))}
            </select>
          </label>
          <Field
            name={`${key}-source`}
            label="该来源整包老额度 credits"
            required
            defaultValue={decimalCredits(value?.source_total ?? 0)}
          />
          <Field
            name={`${key}-current`}
            label="该段当前未消耗老额度 credits"
            required
            defaultValue={decimalCredits(value?.current_credits ?? 0)}
          />
          <Field
            name={`${key}-future`}
            label="该段未发周期额度 credits"
            required
            defaultValue={decimalCredits(value?.future_credits ?? 0)}
          />
          <Field
            name={`${key}-wallet`}
            label="该来源整包可兑余额 credits"
            required
            defaultValue={decimalCredits(value?.wallet_credits ?? 0)}
          />
          <Field
            name={`${key}-paid`}
            label="其中原付费 credits"
            required
            defaultValue={decimalCredits(value?.paid_wallet_credits ?? 0)}
          />
          <Button
            variant="quiet"
            disabled={segments.length <= 1 || props.pending}
            onClick={() => setSegments((items) => items.filter((item) => item.key !== key))}
          >
            {t('移除分段')}
          </Button>
        </fieldset>
      ))}
      <Button
        variant="quiet"
        disabled={segments.length >= 100 || props.pending}
        onClick={() => setSegments((items) => [...items, { key: crypto.randomUUID() }])}
      >
        {t('添加权益分段')}
      </Button>
      <Field
        name="review-note"
        label="核对依据与说明"
        required
        maxLength={2000}
        defaultValue={prior?.note}
      />
      <label className="checkbox-field">
        <input type="checkbox" name="review-checked" defaultChecked={prior?.reviewed} />
        {t('已核对权益与资金来源')}
      </label>
      <label className="checkbox-field">
        <input type="checkbox" name="review-enabled" defaultChecked={prior?.enabled} />
        {t('启用此核定报价')}
      </label>
      <Button type="submit" disabled={props.pending}>
        {t('保存分段核定')}
      </Button>
    </form>
  )
}
