import { expect, test } from '@playwright/test'
import { fixtureAPI } from './fixtures'

const order = {
  id: 99,
  user_id: 1,
  trade_no: 'BBUSR-original-expired-order',
  amount_minor: 500,
  credits: 0,
  currency: 'cny',
  kind: 'blind_box',
  provider: 'epay',
  state: 'paid',
  purchase_type: 'legacy_cash_box_review',
  fulfillment_state: 'requires_review',
  payment_url: '',
  created_at: '2026-10-08T06:56:06Z',
  expires_at: '2026-10-08T06:59:52Z',
  paid_at: '2026-10-09T01:00:00Z',
}

test('legacy money receipt remains an explicit pending-benefits record in user history', async ({
  page,
}) => {
  await fixtureAPI(page)
  await page.route('**/api/commerce/orders?*', (route) =>
    route.fulfill({ json: { success: true, data: [order] } }),
  )
  await page.goto('/orders')
  await expect(page.getByText('付款已收到，待核对旧订单权益', { exact: true })).toBeVisible()
  await expect(page.getByText(/正在核对实际到账金额与旧版盲盒权益；权益尚未发放/)).toBeVisible()
  await expect(page.getByRole('button', { name: '支付', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '取消', exact: true })).toHaveCount(0)
})

test('administrator review shows original provider identity and money without claiming delivery', async ({
  page,
}) => {
  await fixtureAPI(page)
  await page.route('**/api/commerce/admin/orders?*', (route) =>
    route.fulfill({ json: { success: true, data: [order] } }),
  )
  await page.route('**/api/commerce/admin/package-payment-reviews', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            order_id: 99,
            user_id: 1,
            trade_no: order.trade_no,
            provider: 'epay',
            amount_minor: 500,
            currency: 'cny',
            created_at: order.paid_at,
            reason:
              'Legacy cash blind-box payment received; provider transaction original-platform-id; 500 cny minor units. Original draw rights require manual verification; no wallet credits or inventory issued.',
          },
        ],
      },
    }),
  )
  await page.goto('/admin/orders')
  await page.getByRole('button', { name: '查看详情', exact: true }).click()
  await expect(page.getByText('付款已收到，待核对旧订单权益', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '关闭', exact: true }).click()
  await page.getByRole('tab', { name: '人工审核', exact: true }).click()
  await expect(page.getByText(/支付平台交易号与实际金额/)).toBeVisible()
  await expect(page.getByText(/original-platform-id/)).toBeVisible()
})
