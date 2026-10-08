import { useTranslation } from '../../lib/i18n'
import type { Schema } from '../../lib/types'
import { credits } from '../../lib/format'

export function record(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null
}

export function ReferralOrderTerms(props: { order: Schema['Order'] }) {
  const order = props.order
  return <ReferralTerms terms={'referral_terms' in order ? order.referral_terms : undefined} />
}

export function ReferralTerms(props: { terms: unknown }) {
  const { t } = useTranslation()
  const terms = record(props.terms)
  if (!terms || Object.keys(terms).length === 0) return null
  if (terms.legacy_reset_eligible === true)
    return <p className="muted">{t('此订单保留原邀请刷新承诺，付款后按原首购资格核验。')}</p>
  if (terms.eligible !== true) {
    const reason = typeof terms.reason === 'string' ? terms.reason : ''
    const label = (
      {
        program_disabled: '当前消费邀请活动未启用',
        budget_unavailable: '活动预算不足',
        first_purchase_already_claimed: '首购资格已使用',
        no_eligible_inviter: '没有符合条件的邀请关系',
      } as Record<string, string>
    )[reason]
    return (
      <p className="muted">
        {t('此订单不参与新邀请消费奖励')}
        {label ? `：${t(label)}` : ''}
        {t('，不新增刷新次数。')}
      </p>
    )
  }
  const policy = record(terms.policy) ?? terms
  const percent =
    typeof policy?.reward_ppm === 'number' ? `${policy.reward_ppm / 10000}%` : t('订单约定比例')
  const days = typeof policy?.window_days === 'number' ? policy.window_days : t('约定')
  const delay = typeof policy?.delay_days === 'number' ? policy.delay_days : t('约定')
  const cap = policy?.max_reward_credits
  return (
    <p className="muted">
      {t('此订单保留新邀请消费奖励资格：付款后')} {days} {t('天内的实际付费消费按')} {percent}{' '}
      {t('核算，受贡献利润与单笔上限限制，延迟')} {delay} {t('天结算。')}
      {typeof cap === 'string' || typeof cap === 'number' || typeof cap === 'bigint' ? (
        <>
          {t('上限')} {credits(cap)}。
        </>
      ) : null}
      {t('奖励仅邀请者本人消费，转换、换卡和赠送消费不重复发奖，也不新增刷新次数。')}
    </p>
  )
}
