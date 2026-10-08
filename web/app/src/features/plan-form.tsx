import { useTranslation } from '../lib/i18n'
import { useState } from 'react'
import type { Plan } from '../lib/commerce'
import { minorAmount, currencyDigits } from '../lib/commerce'
import { toMicroCredits } from '../lib/format'
import { subscriptionPolicy } from '../lib/subscription-policy'
import { Button, ErrorMessage, Field } from '../components/ui'
import { PlanModelLimits, readPlanModelLimits } from './plan-model-limits'

function decimalMicro(value: Plan['credits']): string {
  const amount = BigInt(value)
  return `${amount / 1_000_000n}.${String(amount % 1_000_000n).padStart(6, '0')}`
}

export function PlanForm(props: {
  plan?: Plan
  pending: boolean
  onSave: (plan: Plan) => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const [error, setError] = useState<Error | null>(null)
  const plan = props.plan
  const [policyVersion, setPolicyVersion] = useState<'legacy' | 'standard_v2'>(
    plan && subscriptionPolicy(plan) !== 'standard_v2' ? 'legacy' : 'standard_v2',
  )
  const fixed = policyVersion === 'standard_v2'
  const membershipTier = plan?.membership_tier ?? 'none'
  return (
    <form
      className="form-panel section"
      onSubmit={(event) => {
        event.preventDefault()
        setError(null)
        try {
          const form = new FormData(event.currentTarget)
          const text = (name: string) => String(form.get(name) ?? '')
          const micro = (name: string) =>
            !text(name) || /^0(?:\.0{1,6})?$/.test(text(name))
              ? 0n
              : BigInt(toMicroCredits(text(name)))
          props.onSave({
            ...plan,
            id: plan?.id ?? 0,
            policy_version: policyVersion,
            name: text('plan-name'),
            price_minor: minorAmount(text('plan-price'), text('plan-currency'), t),
            currency: text('plan-currency').toLowerCase(),
            credits: micro('plan-credits'),
            period_seconds: Number(text('plan-days')) * 86400,
            duration_unit: text('plan-duration-unit'),
            duration_value: Number(text('plan-days')),
            custom_seconds: Number(text('plan-custom-seconds')),
            enabled: form.get('plan-enabled') === 'on',
            group_buy_enabled: !fixed && form.get('plan-group-enabled') === 'on',
            group_buy_target: fixed ? 3 : Number(text('plan-group-target')),
            group_buy_bonus: fixed ? 0n : micro('plan-group-bonus'),
            group_buy_lifetime_seconds: fixed ? 86400 : Number(text('plan-group-hours')) * 3600,
            period_credits: fixed ? 0n : micro('plan-period-credits'),
            reset_period: fixed ? 'never' : text('plan-reset'),
            reset_custom_seconds: fixed ? 0 : Number(text('plan-reset-hours')) * 3600,
            internal_only: form.get('plan-internal') === 'on',
            max_purchase_per_user: Number(text('plan-purchase-limit')),
            group_buy_bonus2_micro: fixed ? 0n : micro('plan-bonus2'),
            group_buy_bonus3_micro: fixed ? 0n : micro('plan-bonus3'),
            group_buy_bonus5_micro: fixed ? 0n : micro('plan-bonus5'),
            plan_type: text('plan-type'),
            lucky_draw_enabled: false,
            membership_tier: fixed ? 'none' : text('plan-membership-tier'),
            fuel_enabled: !fixed && form.get('plan-fuel-enabled') === 'on',
            fuel_unit_price_micro: fixed ? 0n : micro('plan-fuel-price'),
            fuel_min_credits: fixed ? 0n : micro('plan-fuel-min'),
            fuel_credit_step: fixed ? 0n : micro('plan-fuel-step'),
            upgrade_group: text('plan-upgrade-group').trim(),
            model_limits: readPlanModelLimits(form),
          })
        } catch (cause) {
          setError(cause instanceof Error ? cause : new Error('输入无效'))
        }
      }}
    >
      <Field name="plan-name" label="套餐名称" required defaultValue={plan?.name} maxLength={200} />
      <label className="field" htmlFor="plan-policy-version">
        <span>{t('套餐规则版本')}</span>
        <select
          id="plan-policy-version"
          value={policyVersion}
          disabled={!!plan}
          onChange={(event) =>
            setPolicyVersion(event.target.value === 'standard_v2' ? 'standard_v2' : 'legacy')
          }
        >
          <option value="standard_v2">{t('新版固定额度')}</option>
          <option value="legacy">{t('原规则套餐')}</option>
        </select>
      </label>
      <p className="muted full-width">
        {fixed
          ? t(
              '新版与余额同口径扣费，全部额度一次到账。无刷新、周期恢复、拼团赠额或燃料补量；售价与额度由管理员核定，不自动上架建议档位。',
            )
          : t('原规则套餐继续保留原消费与刷新规则，不将已有权益直接改为新版。')}
        {plan && t('版本已冻结；需要另一版本时新建套餐。')}
      </p>
      <Field
        name="plan-price"
        label="支付金额"
        required
        defaultValue={
          plan
            ? (() => {
                const digits = currencyDigits(plan.currency)
                const scale = 10n ** BigInt(digits)
                return `${BigInt(plan.price_minor) / scale}${digits ? `.${String(BigInt(plan.price_minor) % scale).padStart(digits, '0')}` : ''}`
              })()
            : '10.00'
        }
      />
      <Field
        name="plan-currency"
        label="支付币种"
        required
        defaultValue={plan?.currency ?? 'usd'}
      />
      <Field
        name="plan-credits"
        label="套餐 credits"
        required
        defaultValue={plan ? decimalMicro(plan.credits) : '100'}
      />
      <label className="field" htmlFor="plan-duration-unit">
        <span>{t('有效期单位')}</span>
        <select
          id="plan-duration-unit"
          name="plan-duration-unit"
          defaultValue={plan?.duration_unit ?? 'day'}
        >
          <option value="day">{t('天')}</option>
          <option value="hour">{t('小时')}</option>
          <option value="month">{t('自然月')}</option>
          <option value="year">{t('自然年')}</option>
          <option value="custom">{t('自定义秒数')}</option>
        </select>
      </label>
      <Field
        name="plan-days"
        label="有效期数量"
        type="number"
        min={1}
        required
        defaultValue={plan?.duration_value ?? 90}
      />
      <Field
        name="plan-custom-seconds"
        label="自定义有效秒数"
        type="number"
        min={0}
        defaultValue={Number(plan?.custom_seconds ?? 2592000)}
      />
      {!fixed && (
        <>
          <Field
            name="plan-period-credits"
            label="每周期 credits（0 为不重置）"
            required
            defaultValue={plan ? decimalMicro(plan.period_credits) : '0'}
          />
          <label className="field" htmlFor="plan-reset">
            <span>{t('重置周期')}</span>
            <select name="plan-reset" id="plan-reset" defaultValue={plan?.reset_period ?? 'never'}>
              <option value="never">{t('不重置')}</option>
              <option value="daily">{t('每天')}</option>
              <option value="weekly">{t('每周')}</option>
              <option value="monthly">{t('每月')}</option>
              <option value="custom">{t('自定义')}</option>
            </select>
          </label>
          <Field
            name="plan-reset-hours"
            label="自定义重置小时"
            type="number"
            min={0}
            defaultValue={Number(plan?.reset_custom_seconds ?? 0) / 3600}
          />
        </>
      )}
      <Field
        name="plan-purchase-limit"
        label="每人限购次数（0 为不限）"
        type="number"
        min={0}
        defaultValue={plan?.max_purchase_per_user ?? 0}
      />
      <label className="checkbox-field">
        <input type="checkbox" name="plan-internal" defaultChecked={plan?.internal_only ?? false} />
        {t('仅内部使用')}
      </label>
      <label className="checkbox-field">
        <input type="checkbox" name="plan-enabled" defaultChecked={plan?.enabled ?? false} />
        {t('上架套餐')}
      </label>
      {!fixed && (
        <>
          <label className="checkbox-field">
            <input
              type="checkbox"
              name="plan-group-enabled"
              defaultChecked={plan?.group_buy_enabled ?? false}
            />
            {t('允许拼团')}
          </label>
          <Field
            name="plan-group-target"
            label="成团人数"
            type="number"
            min={2}
            required
            defaultValue={Number(plan?.group_buy_target ?? 3)}
          />
          <Field
            name="plan-group-bonus"
            label="拼团奖励 credits"
            required
            defaultValue={plan ? decimalMicro(plan.group_buy_bonus) : '0'}
          />
          <Field
            name="plan-group-hours"
            label="拼团有效小时"
            type="number"
            min={1}
            required
            defaultValue={Number(plan?.group_buy_lifetime_seconds ?? 86400) / 3600}
          />
          <Field
            name="plan-bonus2"
            label="二人团奖励 credits"
            required
            defaultValue={decimalMicro(plan?.group_buy_bonus2_micro ?? 0)}
          />
          <Field
            name="plan-bonus3"
            label="三人团奖励 credits"
            required
            defaultValue={decimalMicro(plan?.group_buy_bonus3_micro ?? 0)}
          />
          <Field
            name="plan-bonus5"
            label="五人团奖励 credits"
            required
            defaultValue={decimalMicro(plan?.group_buy_bonus5_micro ?? 0)}
          />
        </>
      )}
      <Field
        name="plan-type"
        label="套餐类型"
        defaultValue={plan?.plan_type ?? ''}
        placeholder="monthly 为月卡"
      />
      {!fixed && (
        <>
          <label className="field" htmlFor="plan-membership-tier">
            <span>{t('旧套餐权益等级')}</span>
            <select
              id="plan-membership-tier"
              name="plan-membership-tier"
              defaultValue={membershipTier}
            >
              {!['none', 'lite', 'standard', 'pro', 'ultra'].includes(membershipTier) && (
                <option value={membershipTier}>{membershipTier || t('未设置')}</option>
              )}
              <option value="none">{t('无等级')}</option>
              <option value="lite">Lite</option>
              <option value="standard">Standard</option>
              <option value="pro">Pro</option>
              <option value="ultra">Ultra</option>
            </select>
          </label>
        </>
      )}
      {!fixed && (
        <>
          <label className="checkbox-field">
            <input
              type="checkbox"
              name="plan-fuel-enabled"
              defaultChecked={plan?.fuel_enabled ?? false}
            />
            {t('允许购买燃料补充额度')}
          </label>
          <Field
            name="plan-fuel-price"
            label="每 credit 燃料价格（支付币种）"
            required
            defaultValue={decimalMicro(plan?.fuel_unit_price_micro ?? 0)}
          />
          <Field
            name="plan-fuel-min"
            label="最低燃料 credits"
            required
            defaultValue={decimalMicro(plan?.fuel_min_credits ?? 0)}
          />
          <Field
            name="plan-fuel-step"
            label="燃料步长 credits"
            required
            defaultValue={decimalMicro(plan?.fuel_credit_step ?? 0)}
          />
        </>
      )}
      <Field
        name="plan-upgrade-group"
        label="订阅期间升级分组（留空保持原分组）"
        defaultValue={plan?.upgrade_group ?? ''}
        maxLength={64}
      />
      <PlanModelLimits limits={plan?.model_limits} />
      <ErrorMessage error={error} />
      <div className="row-actions">
        <Button type="submit" disabled={props.pending}>
          {t('保存')}
        </Button>
        <Button type="button" variant="quiet" onClick={props.onCancel}>
          {t('取消')}
        </Button>
      </div>
    </form>
  )
}
