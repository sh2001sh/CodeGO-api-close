import { Link } from '@tanstack/react-router'
import { useSuspenseQuery } from '@tanstack/react-query'
import { api, unwrap } from '../lib/api'
import { resourceOptions } from '../lib/queries'
import { useTranslation } from '../lib/i18n'
import { credits, date } from '../lib/format'
import { paymentAmount } from '../lib/commerce'
import { subscriptionPolicyLabel, validSubscription } from '../lib/subscription-policy'
import { DataTable } from '../components/data-table'
import { Callout, PageHeader, Status } from '../components/ui'

/**
 * `/api/packages/*` is a route alias of the same subscription system exposed
 * at `/api/subscription/*` and `/api/commerce/orders` (confirmed by reading
 * v3/internal/commerce/subscription_http.go and orders.go: `packages/public`
 * calls the same handler as `subscription/plans`, `packages/my-subscription`
 * calls the same handler as `subscription/self`, and purchase/renew/upgrade
 * all funnel into Service.Create with the same CreatePackageCheckout path
 * the wallet page already uses). This page is therefore a read-only
 * overview, not a second checkout flow — all purchase/renew/upgrade actions
 * stay on /wallet so there is exactly one place that calls the payment API.
 */
export default function PackagesPage() {
  const { t } = useTranslation()
  const plans = useSuspenseQuery(
    resourceOptions('packages-public', (signal) =>
      api.GET('/api/packages/public', { signal }).then((result) => unwrap(result)),
    ),
  ).data
  const subscriptions = useSuspenseQuery(
    resourceOptions('packages-my-subscription', (signal) =>
      api.GET('/api/packages/my-subscription', { signal }).then((result) => unwrap(result)),
    ),
  ).data
  const active = (subscriptions ?? []).filter((item) => validSubscription(item))
  return (
    <>
      <PageHeader
        title="套餐包"
        description="套餐包与钱包页的订阅是同一套服务，这里只做总览；购买、续费与升级请前往钱包页完成。"
        action={
          <Link className="button button-primary" to="/wallet">
            {t('前往钱包购买')}
          </Link>
        }
      />
      <Callout tone="info" title={t('说明')}>
        {t(
          '套餐包不是独立产品线：它与“钱包”页的订阅共用同一套计划、同一笔订单和同一条支付链路，这里仅用于查看可购套餐与当前订阅状态。',
        )}
      </Callout>
      <section className="section">
        <h2>{t('我的订阅')}</h2>
        <DataTable
          rows={subscriptions ?? []}
          rowKey={(row) => row.id}
          empty="暂无订阅记录。前往钱包页可购买套餐。"
          columns={[
            {
              label: '套餐',
              render: (row) =>
                plans?.find((plan) => String(plan.id) === String(row.plan_id))?.name ?? row.plan_id,
            },
            { label: '余额', render: (row) => credits(row.balance), numeric: true },
            { label: '规则', render: (row) => t(subscriptionPolicyLabel(row)) },
            { label: '状态', render: (row) => <Status value={row.state} /> },
            { label: '到期时间', render: (row) => date(row.expires_at) },
          ]}
        />
      </section>
      <section className="section">
        <h2>{t('可购套餐')}</h2>
        <DataTable
          rows={plans ?? []}
          rowKey={(row) => row.id}
          empty="暂无在售套餐。"
          columns={[
            { label: '名称', render: (row) => row.name },
            { label: '规则', render: (row) => t(subscriptionPolicyLabel(row)) },
            { label: '额度', render: (row) => credits(row.credits), numeric: true },
            {
              label: '价格',
              render: (row) => paymentAmount(row.price_minor, row.currency),
              numeric: true,
            },
            {
              label: '操作',
              render: () => (
                <Link className="button button-quiet" to="/wallet">
                  {active.length ? t('续费 / 升级') : t('购买')}
                </Link>
              ),
            },
          ]}
        />
      </section>
    </>
  )
}
