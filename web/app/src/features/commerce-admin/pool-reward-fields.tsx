import { useState } from 'react'
import { Button, ErrorMessage } from '../../components/ui'
import { useTranslation } from '../../lib/i18n'
import type { Schema } from '../../lib/types'
import { weightPercentage } from '../blind-box/presentation'
import { newStandardPolicy, parsePoolConfig, poolConfigJSON } from './pool-draft'

type Reward = Schema['MarketplaceReward']
type IntegerField =
  'weight' | 'amount_micro' | 'minimum_micro' | 'maximum_micro' | 'step_micro' | 'plan_id'

const newReward = (): Reward => ({
  kind: 'credits',
  title: '',
  weight: 1,
  amount_micro: 1000000,
  multiplier_ppm: 0,
  duration_seconds: 0,
  plan_id: 0,
})

/** Common rewards are edited as rows; legacy-specific properties stay in the exact JSON. */
export function PoolRewardFields({ initial }: { initial: readonly Reward[] }) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<Reward[]>(() => [...initial])
  const [advanced, setAdvanced] = useState(false)
  const [json, setJSON] = useState('')
  const [error, setError] = useState<Error | null>(null)
  const update = (index: number, field: keyof Reward, value: string) => {
    setRows((previous) =>
      previous.map((row, i) => (i === index ? { ...row, [field]: value } : row)),
    )
  }
  let total: bigint | undefined
  try {
    const weights = rows.map((row) => String(row.weight))
    if (weights.every((value) => /^[1-9]\d*$/.test(value)))
      total = weights.reduce((sum, value) => sum + BigInt(value), 0n)
  } catch {
    total = undefined
  }
  const integer = (row: Reward, index: number, field: IntegerField, label: string) => (
    <label className="field" htmlFor={`pool-reward-${index}-${field}`}>
      <span>{t(label)}</span>
      <input
        id={`pool-reward-${index}-${field}`}
        dir="ltr"
        inputMode="numeric"
        pattern={field === 'weight' || field === 'plan_id' ? '[1-9][0-9]*' : '[0-9]+'}
        required
        value={String(row[field] ?? 0)}
        onChange={(event) => update(index, field, event.target.value)}
      />
    </label>
  )
  return (
    <section className="pool-reward-fields full-width" aria-labelledby="pool-rewards-heading">
      <div className="page-header">
        <h3 id="pool-rewards-heading">{t('奖励配置')}</h3>
        <Button
          variant="quiet"
          type="button"
          onClick={() => {
            setError(null)
            if (!advanced) {
              setJSON(poolConfigJSON(rows))
              setAdvanced(true)
              return
            }
            try {
              const parsed = parsePoolConfig(
                json,
                '{}',
                poolConfigJSON(newStandardPolicy),
                'credits',
              )
              setRows(parsed.rewards)
              setAdvanced(false)
            } catch (failure) {
              setError(
                failure instanceof Error
                  ? failure
                  : new Error('奖励配置无效，请检查类型、金额和权重'),
              )
            }
          }}
        >
          {t(advanced ? '返回逐项编辑' : '编辑完整奖励 JSON')}
        </Button>
      </div>
      <p className="muted">
        {t(
          '逐项编辑余额和套餐奖励；金额以 micro-credits 保存，1 credit = 1,000,000 micro-credits。旧道具的完整字段保留在高级配置中。',
        )}
      </p>
      <ErrorMessage error={error} />
      {advanced ? (
        <label className="field" htmlFor="pool-rewards">
          <span>{t('奖励配置 JSON')}</span>
          <textarea
            id="pool-rewards"
            name="pool-rewards"
            rows={14}
            required
            spellCheck={false}
            value={json}
            onChange={(event) => setJSON(event.target.value)}
          />
        </label>
      ) : (
        <>
          <input type="hidden" name="pool-rewards" value={poolConfigJSON(rows)} />
          {rows.map((row, index) => (
            <fieldset key={index} className="pool-reward-row">
              <legend>
                {t('奖励')} {index + 1} ·{' '}
                {total === undefined
                  ? t('概率暂不可用')
                  : (weightPercentage(row.weight, total) ?? t('概率暂不可用'))}
              </legend>
              <div className="pool-reward-grid">
                <label className="field" htmlFor={`pool-reward-${index}-title`}>
                  <span>{t('展示名称')}</span>
                  <input
                    id={`pool-reward-${index}-title`}
                    required
                    maxLength={200}
                    value={row.title}
                    onChange={(event) => update(index, 'title', event.target.value)}
                  />
                </label>
                <label className="field" htmlFor={`pool-reward-${index}-kind`}>
                  <span>{t('奖励类型')}</span>
                  <select
                    id={`pool-reward-${index}-kind`}
                    value={row.kind}
                    onChange={(event) => update(index, 'kind', event.target.value)}
                  >
                    <option value="credits">{t('消费额度')}</option>
                    <option value="subscription">{t('赠送订阅套餐')}</option>
                    {!['credits', 'subscription'].includes(row.kind) && (
                      <option value={row.kind}>
                        {row.kind} · {t('历史奖励')}
                      </option>
                    )}
                  </select>
                </label>
                {integer(row, index, 'weight', '权重（精确整数）')}
                {row.kind === 'credits' && (
                  <>
                    {integer(row, index, 'amount_micro', '固定额度（micro-credits）')}
                    <details className="full-width">
                      <summary>{t('区间奖励设置')}</summary>
                      <p className="muted">
                        {t('固定额度大于 0 时使用固定额度；固定额度为 0 时按原区间规则抽取。')}
                      </p>
                      <div className="pool-reward-grid">
                        {integer(row, index, 'minimum_micro', '最低额度（micro-credits）')}
                        {integer(row, index, 'maximum_micro', '最高额度（micro-credits）')}
                        {integer(row, index, 'step_micro', '区间步长（micro-credits）')}
                      </div>
                    </details>
                  </>
                )}
                {row.kind === 'subscription' && integer(row, index, 'plan_id', '赠送套餐 ID')}
                {!['credits', 'subscription'].includes(row.kind) && (
                  <p className="muted full-width">
                    {t('此历史奖励保留原规格；请使用完整奖励 JSON 修改专有字段。')}
                  </p>
                )}
              </div>
              <Button
                variant="quiet"
                type="button"
                disabled={rows.length <= 1}
                onClick={() => setRows((previous) => previous.filter((_, i) => i !== index))}
              >
                {t('移除此奖励')}
              </Button>
            </fieldset>
          ))}
          <Button
            variant="secondary"
            type="button"
            disabled={rows.length >= 1000}
            onClick={() => setRows((previous) => [...previous, newReward()])}
          >
            {t('添加奖励')}
          </Button>
        </>
      )}
    </section>
  )
}
