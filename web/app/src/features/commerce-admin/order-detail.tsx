import type { Schema } from '../../lib/types'
import { useTranslation } from '../../lib/i18n'
import { credits, date } from '../../lib/format'
import { paymentAmount } from '../../lib/commerce'
import { Status } from '../../components/ui'
import { LegacyCashBoxReview } from '../commerce/legacy-cash-box-review'

type Order = Schema['Order']

/** Read-only detail view for one order row; data already comes from the list response. */
export function OrderDetail(props: { order: Order }) {
  const { t } = useTranslation()
  const o = props.order
  return (
    <dl className="kv">
      <div>
        <dt>{t('用户 ID')}</dt>
        <dd>{String(o.user_id)}</dd>
      </div>
      <div>
        <dt>{t('状态')}</dt>
        <dd>
          <Status value={o.state} />
        </dd>
      </div>
      <div>
        <dt>{t('履约状态')}</dt>
        <dd>
          {o.fulfillment_state || '—'}
          <LegacyCashBoxReview order={o} />
        </dd>
      </div>
      <div>
        <dt>{t('类型')}</dt>
        <dd>{o.kind}</dd>
      </div>
      <div>
        <dt>{t('支付方式')}</dt>
        <dd>{o.provider}</dd>
      </div>
      <div>
        <dt>{t('支付金额')}</dt>
        <dd>{paymentAmount(o.amount_minor, o.currency)}</dd>
      </div>
      <div>
        <dt>{t('额度')}</dt>
        <dd>{credits(o.credits)}</dd>
      </div>
      <div>
        <dt>{t('套餐')}</dt>
        <dd>{o.plan_snapshot?.name ?? (o.plan_id ? String(o.plan_id) : '—')}</dd>
      </div>
      <div>
        <dt>{t('购买类型')}</dt>
        <dd>{o.purchase_type || '—'}</dd>
      </div>
      <div>
        <dt>{t('计价策略')}</dt>
        <dd>{o.policy_version}</dd>
      </div>
      <div>
        <dt>{t('拼团')}</dt>
        <dd>
          {o.group_buy_enabled
            ? String(o.group_buy_target) +
              ' ' +
              String(t('人')) +
              ' · ' +
              String(t('奖励')) +
              ' ' +
              String(credits(o.group_buy_bonus))
            : t('拼团未开启')}
        </dd>
      </div>
      <div>
        <dt>{t('创建时间')}</dt>
        <dd>{date(o.created_at)}</dd>
      </div>
      <div>
        <dt>{t('到期时间')}</dt>
        <dd>{date(o.expires_at)}</dd>
      </div>
      <div>
        <dt>{t('支付时间')}</dt>
        <dd>{date(o.paid_at)}</dd>
      </div>
      <div>
        <dt>{t('服务商订单号')}</dt>
        <dd>{o.provider_reference || '—'}</dd>
      </div>
    </dl>
  )
}
