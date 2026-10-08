import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useToast } from '../../hooks/use-toast'
import { useTranslation } from '../../lib/i18n'
import { date } from '../../lib/format'
import { paymentAmount } from '../../lib/commerce'
import { DataTable } from '../../components/data-table'
import { Button, Callout, confirmAction, ErrorMessage, Loading } from '../../components/ui'

export const packagePaymentReviewsOptions = () =>
  resourceOptions('package-payment-reviews', (signal) =>
    api.GET('/api/commerce/admin/package-payment-reviews', { signal }).then(unwrap),
  )

/**
 * Manual review queue for package payments the settlement pipeline could
 * not safely confirm. The backend only exposes one action here: resolve.
 * It succeeds only when the order is already in a verified refunded state
 * server-side, so this screen cannot invent a refund or grant credits —
 * it just acknowledges that the admin confirmed the refund elsewhere.
 */
export function PackagePaymentReviews() {
  const { t } = useTranslation()
  const toast = useToast((s) => s.add)
  const client = useQueryClient()
  const reviews = useQuery(packagePaymentReviewsOptions())
  const resolve = useMutation({
    mutationFn: (orderID: string | number | bigint) =>
      api.POST('/api/commerce/admin/package-payment-reviews/{id}/resolve', {
        params: { path: { id: orderID } },
      }),
    onSuccess: () => {
      toast(t('已处理'), 'success')
      void client.invalidateQueries({ queryKey: ['package-payment-reviews'] })
    },
    onError: (error: Error) => toast(error.message, 'error'),
  })
  const resolveReview = async (orderID: string | number | bigint) => {
    const confirmed = await confirmAction({
      title: '标记审核已处理',
      description:
        '仅在已经通过支付渠道后台确认该笔款项全额退款后才能标记。未退款的订单会被服务器拒绝，不会提示成功。',
      confirmLabel: '确认已退款，标记完成',
      danger: true,
    })
    if (confirmed) resolve.mutate(orderID)
  }
  return (
    <div className="section">
      <Callout tone="warning" title={t('人工审核')}>
        {t(
          '这里收录了支付结算流程无法自动确认的套餐订单。标记完成前，请先在支付渠道后台核实该笔退款是否已经处理；服务器只在订单已退款时接受标记。',
        )}
      </Callout>
      <ErrorMessage error={reviews.error ?? resolve.error} />
      {reviews.isPending && <Loading />}
      <DataTable
        rows={reviews.data ?? []}
        rowKey={(row) => String(row.order_id)}
        empty="暂无待审核订单"
        columns={[
          { label: '订单', render: (row) => String(row.order_id) },
          { label: '用户 ID', render: (row) => String(row.user_id) },
          { label: '订单号', render: (row) => <code>{row.trade_no}</code> },
          { label: '支付方式', render: (row) => row.provider },
          {
            label: '金额',
            render: (row) => paymentAmount(row.amount_minor, row.currency),
            numeric: true,
          },
          { label: '原因', render: (row) => row.reason },
          { label: '产生时间', render: (row) => date(row.created_at) },
          {
            label: '操作',
            render: (row) => (
              <Button
                variant="danger"
                disabled={resolve.isPending}
                onClick={() => resolveReview(row.order_id)}
              >
                {t('标记已处理')}
              </Button>
            ),
          },
        ]}
      />
    </div>
  )
}
