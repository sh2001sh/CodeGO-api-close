import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

const event = {
  id: 'historical-event-9007199254740993',
  request_id: 'retained-request',
  source: 'prompt_guard',
  decision: 'blocked',
  risk_code: 'MODEL_POLICY',
  severity: 'high',
  channel_id: '9007199254740993',
  marketplace_channel_id: 'original-channel',
  review_status: 'unreviewed',
  review_note: '迁入备注',
  created_at: '2025-01-01T01:02:03Z',
}
test.beforeEach(async ({ page }) => fixtureAPI(page))

test('owner reviews retained string IDs with notes, safe failure retry and server-side filtering', async ({
  page,
}) => {
  let result = { ...event }
  const updates: { id: string; body: { review_status: string; review_note: string } }[] = []
  await page.route('**/api/marketplace/security-audit/events**', (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() === 'PATCH') {
      updates.push({
        id: decodeURIComponent(url.pathname.split('/').at(-1)!),
        body: route.request().postDataJSON(),
      })
      if (updates.length === 1)
        return route.fulfill({
          status: 503,
          json: { success: false, message: '审核服务暂时不可用' },
        })
      result = { ...result, ...updates[0].body }
      return route.fulfill({ json: { success: true, data: result } })
    }
    const status = url.searchParams.get('review_status')
    return route.fulfill({
      json: {
        success: true,
        data: {
          items: !status || status === result.review_status ? [result] : [],
          total: !status || status === result.review_status ? 1 : 0,
          page: 1,
          page_size: 20,
        },
      },
    })
  })
  await page.goto('/my-channels')
  const audit = page.getByRole('region', { name: '安全审计', exact: true })
  await expect(audit.getByText('MODEL_POLICY', { exact: true })).toBeVisible()
  await expect(audit.getByText('prompt_guard', { exact: true })).toBeVisible()
  await expect(audit.getByText('blocked', { exact: true })).toBeVisible()
  await audit.getByRole('button', { name: '审核', exact: true }).click()
  await expect(page.getByRole('textbox', { name: '审核备注', exact: true })).toHaveValue('迁入备注')
  await page.getByRole('textbox', { name: '审核备注', exact: true }).fill('已复核，保留原始记录')
  await page.getByRole('combobox', { name: '审核结果', exact: true }).selectOption('resolved')
  await page.getByRole('button', { name: '保存审核', exact: true }).click()
  await expect(audit.getByRole('alert')).toHaveText('审核服务暂时不可用')
  await page.getByRole('button', { name: '保存审核', exact: true }).click()
  await expect(audit.getByRole('status').filter({ hasText: '审核已保存' })).toHaveText(
    '审核已保存。',
  )
  expect(updates).toHaveLength(2)
  expect(updates[0]).toEqual(updates[1])
  expect(updates[0]).toEqual({
    id: event.id,
    body: { review_status: 'resolved', review_note: '已复核，保留原始记录' },
  })
  const filtered = page.waitForRequest(
    (request) => new URL(request.url()).searchParams.get('review_status') === 'resolved',
  )
  await audit.getByRole('combobox', { name: '审核状态', exact: true }).selectOption('resolved')
  await filtered
  await expect(audit.getByText('已复核，保留原始记录', { exact: true })).toBeVisible()
  await expect(audit.getByRole('link', { name: '导出筛选结果', exact: true })).toHaveAttribute(
    'href',
    '/api/marketplace/security-audit/events/export?review_status=resolved',
  )
})

test('administrator paginates retained audit events and sends original string IDs to the admin review endpoint', async ({
  page,
}) => {
  await page.route('**/api/marketplace/admin/security-audit/events**', (route) => {
    const url = new URL(route.request().url())
    if (route.request().method() === 'PATCH')
      return route.fulfill({ json: { success: true, data: event } })
    const number = Number(url.searchParams.get('page'))
    return route.fulfill({
      json: {
        success: true,
        data: {
          items: [
            {
              ...event,
              id: number === 2 ? 'second-original-id' : event.id,
              risk_code: number === 2 ? 'SECOND_PAGE' : 'MODEL_POLICY',
            },
          ],
          total: 21,
          page: number,
          page_size: 20,
        },
      },
    })
  })
  await page.goto('/market-admin')
  const audit = page.getByRole('region', { name: '安全审计', exact: true })
  await expect(audit.getByText('第 1 页，共 21 条', { exact: true })).toBeVisible()
  await audit.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(audit.getByText('SECOND_PAGE', { exact: true })).toBeVisible()
  await expect(audit.getByRole('button', { name: '下一页', exact: true })).toBeDisabled()
  await audit.getByRole('button', { name: '审核', exact: true }).click()
  await audit
    .getByRole('combobox', { name: '审核结果', exact: true })
    .selectOption('false_positive')
  const sent = page.waitForRequest(
    '**/api/marketplace/admin/security-audit/events/second-original-id',
  )
  await audit.getByRole('button', { name: '保存审核', exact: true }).click()
  const request = await sent
  expect(request.method()).toBe('PATCH')
  expect(request.postDataJSON()).toEqual({
    review_status: 'false_positive',
    review_note: '迁入备注',
  })
})
