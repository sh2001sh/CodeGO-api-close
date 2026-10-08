import { useState } from 'react'
import type { Schema } from '../../lib/types'
import { decimalCredits } from '../commerce/amounts'
import { toMicroCredits } from '../../lib/format'
import { useTranslation } from '../../lib/i18n'
import { Button, Field } from '../../components/ui'

type Reward = Schema['MarketplaceReward']

const kinds = [
  { value: 'credits', label: '现金奖励（credits）' },
  { value: 'multiplier', label: '倍率道具' },
  { value: 'subscription', label: '赠送订阅套餐' },
  { value: 'topup_discount', label: '充值折扣券' },
  { value: 'subscription_discount', label: '订阅折扣券' },
  { value: 'extra_draw', label: '额外抽取一次' },
] as const

type Row = { id: string; reward: Reward }

const emptyReward = (): Reward => ({
  kind: 'credits',
  title: '',
  weight: 1,
  amount_micro: 0,
  multiplier_ppm: 1000000,
  duration_seconds: 0,
  plan_id: 0,
})

export function rewardRows(rewards: readonly Reward[] | null | undefined): Row[] {
  return (rewards ?? []).map((reward) => ({ id: crypto.randomUUID(), reward }))
}

/** Reads one prefix's dynamic reward rows back out of submitted FormData. */
export function readRewards(form: FormData, prefix: string): Reward[] {
  const ids = form.getAll(`${prefix}-id`).map(String)
  return ids.map((_id, index) => {
    const field = (name: string) => String(form.getAll(`${prefix}-${name}`)[index] ?? '').trim()
    const kind = field('kind')
    const amountText = field('amount')
    return {
      kind,
      title: field('title'),
      weight: Number(field('weight') || '1'),
      amount_micro: kind === 'credits' && amountText ? BigInt(toMicroCredits(amountText)) : 0,
      multiplier_ppm: Number(field('multiplier')) * 10000 || 0,
      duration_seconds: Number(field('duration')) * 3600 || 0,
      plan_id: Number(field('plan-id')) || 0,
      discount_rate_ppm: Number(field('discount')) * 10000 || 0,
    }
  })
}

/** One editable reward row; fields shown depend on the selected kind. */
function RewardRow(props: {
  prefix: string
  row: Row
  onChange: (reward: Reward) => void
  onRemove: () => void
}) {
  const { t } = useTranslation()
  const { reward } = props.row
  const set = (patch: Partial<Reward>) => props.onChange({ ...reward, ...patch })
  const kindFieldID = `${props.prefix}-kind-${props.row.id}`
  return (
    <div className="form-panel">
      <input type="hidden" name={`${props.prefix}-id`} value={props.row.id} />
      <label className="field" htmlFor={kindFieldID}>
        <span>{t('奖励类型')}</span>
        <select
          id={kindFieldID}
          name={`${props.prefix}-kind`}
          value={reward.kind}
          onChange={(event) => set({ kind: event.target.value })}
        >
          {kinds.map((kind) => (
            <option key={kind.value} value={kind.value}>
              {t(kind.label)}
            </option>
          ))}
        </select>
      </label>
      <Field name={`${props.prefix}-title`} label="展示名称" required defaultValue={reward.title} />
      <Field
        name={`${props.prefix}-weight`}
        label="权重"
        type="number"
        min={1}
        required
        defaultValue={String(reward.weight)}
      />
      {reward.kind === 'credits' && (
        <Field
          name={`${props.prefix}-amount`}
          label="奖励 credits"
          required
          defaultValue={decimalCredits(reward.amount_micro)}
        />
      )}
      {reward.kind === 'multiplier' && (
        <>
          <Field
            name={`${props.prefix}-multiplier`}
            label="倍率（%）"
            type="number"
            min={0}
            step="0.0001"
            required
            defaultValue={String(Number(reward.multiplier_ppm) / 10000)}
          />
          <Field
            name={`${props.prefix}-duration`}
            label="持续小时"
            type="number"
            min={1}
            required
            defaultValue={String(Number(reward.duration_seconds) / 3600)}
          />
        </>
      )}
      {reward.kind === 'subscription' && (
        <Field
          name={`${props.prefix}-plan-id`}
          label="赠送套餐 ID"
          type="number"
          min={1}
          required
          defaultValue={String(reward.plan_id)}
        />
      )}
      {(reward.kind === 'topup_discount' || reward.kind === 'subscription_discount') && (
        <Field
          name={`${props.prefix}-discount`}
          label="折扣比例（%）"
          type="number"
          min={0}
          max={100}
          step="0.0001"
          required
          defaultValue={String(Number(reward.discount_rate_ppm) / 10000)}
        />
      )}
      <Button variant="quiet" type="button" onClick={props.onRemove}>
        {t('移除此奖励')}
      </Button>
    </div>
  )
}

/** Dynamic add/remove list of reward rows sharing one form-field prefix. */
export function RewardRows(props: {
  prefix: string
  legend: string
  initial: readonly Reward[] | null | undefined
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<Row[]>(() => rewardRows(props.initial))
  return (
    <fieldset className="full-width section">
      <legend>{t(props.legend)}</legend>
      {rows.map((row) => (
        <RewardRow
          key={row.id}
          prefix={props.prefix}
          row={row}
          onChange={(reward) =>
            setRows((values) =>
              values.map((item) => (item.id === row.id ? { ...item, reward } : item)),
            )
          }
          onRemove={() => setRows((values) => values.filter((item) => item.id !== row.id))}
        />
      ))}
      <Button
        variant="quiet"
        type="button"
        onClick={() =>
          setRows((values) => [...values, { id: crypto.randomUUID(), reward: emptyReward() }])
        }
      >
        {t('添加奖励')}
      </Button>
    </fieldset>
  )
}
