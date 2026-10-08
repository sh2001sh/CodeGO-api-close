import { test, expect } from '@playwright/test'
import { fixtureAPI, user } from './fixtures'

const usage = {
  id: '9007199254740993',
  created_at: '2026-09-30T08:00:00Z',
  account_id: 1,
  user_id: 1,
  key_id: 1,
  channel_id: 1,
  amount: '1500000',
  prompt_tokens: 100,
  completion_tokens: 20,
  cached_tokens: 0,
  estimated: false,
  request_id: 'req-fixture-1',
  model: 'gpt-4o',
  terminal: 'Completed',
}
const usagePage = { items: [usage], next_cursor: 'cursor-2', has_more: true, page_size: 50 }
const summary = {
  requests: 42,
  amount: '12300000',
  prompt_tokens: 1000,
  completion_tokens: 200,
  cached_tokens: 10,
}

test.beforeEach(async ({ page }) => fixtureAPI(page))

test.describe('usage logs (self)', () => {
  test('renders stats, filters by model and opens the detail drawer', async ({ page }) => {
    await page.route('**/api/log/self/stat**', (route) =>
      route.fulfill({ json: { success: true, data: summary } }),
    )
    const requests: string[] = []
    await page.route('**/api/log/self**', (route) => {
      if (route.request().url().includes('/stat')) return route.fallback()
      requests.push(route.request().url())
      return route.fulfill({ json: { success: true, data: usagePage } })
    })
    await page.goto('/usage-logs')
    await expect(page.getByRole('heading', { name: '使用日志', exact: true })).toBeVisible()
    await expect(page.getByText('42', { exact: true })).toBeVisible()
    await page.getByLabel('模型', { exact: true }).fill('gpt-4o')
    const filtered = page.waitForRequest((request) => request.url().includes('model=gpt-4o'))
    await page.getByRole('button', { name: '筛选', exact: true }).click()
    await filtered
    await page.getByRole('row').filter({ hasText: 'gpt-4o' }).click()
    await expect(page.getByRole('heading', { name: '调用详情', exact: true })).toBeVisible()
    await expect(page.getByText('req-fixture-1').first()).toBeVisible()
  })

  test('shows the empty state when there are no records', async ({ page }) => {
    await page.route('**/api/log/self/stat**', (route) =>
      route.fulfill({ json: { success: true, data: { ...summary, requests: 0, amount: '0' } } }),
    )
    await page.route('**/api/log/self**', (route) => {
      if (route.request().url().includes('/stat')) return route.fallback()
      return route.fulfill({
        json: { success: true, data: { items: [], has_more: false, page_size: 50 } },
      })
    })
    await page.goto('/usage-logs')
    await expect(page.getByText('暂无使用记录', { exact: true })).toBeVisible()
  })
})

test.describe('admin logs', () => {
  test('renders with the user filter and sends it as a query param', async ({ page }) => {
    await page.route('**/api/log/stat**', (route) =>
      route.fulfill({ json: { success: true, data: summary } }),
    )
    await page.route('**/api/log/**', (route) => {
      const url = route.request().url()
      if (url.includes('/stat') || url.includes('/self') || url.includes('/export'))
        return route.fallback()
      return route.fulfill({ json: { success: true, data: usagePage } })
    })
    await page.goto('/admin/logs')
    await expect(page.getByRole('heading', { name: '全站日志', exact: true })).toBeVisible()
    await page.getByLabel('用户 ID', { exact: true }).fill('42')
    const filtered = page.waitForRequest(
      (request) => request.url().includes('/api/log/') && request.url().includes('user_id=42'),
    )
    await page.getByRole('button', { name: '筛选', exact: true }).click()
    await filtered
  })

  test('ordinary users cannot open the admin logs page', async ({ page }) => {
    await page.route('**/api/user/self', (route) =>
      route.fulfill({ json: { success: true, data: { ...user, role: 'user' } } }),
    )
    await page.goto('/admin/logs')
    await expect(page.getByRole('alert')).toHaveText('无权执行此操作')
  })
})

test.describe('request audit', () => {
  const requestAudit = {
    request_id: 'req-audit-1',
    trace_id: 'trace-1',
    user_id: 1,
    key_id: 1,
    model: 'gpt-4o',
    group_name: 'default',
    protocol: 'openai',
    request_type: 'chat',
    status: 'succeeded',
    counted_in_success_rate: true,
    billable: true,
    amount: '1500000',
    prompt_tokens: 100,
    completion_tokens: 20,
    final_channel_id: 1,
    attempts_count: 1,
    retry_count: 0,
    status_code: 200,
    error_code: '',
    started_at: '2026-09-30T08:00:00Z',
    completed_at: '2026-09-30T08:00:01Z',
  }

  test('switches tabs, opens a request detail with its attempts timeline', async ({ page }) => {
    await page.route('**/api/audit/requests**', (route) => {
      if (route.request().url().includes('/attempts')) return route.fallback()
      return route.fulfill({
        json: {
          success: true,
          data: { items: [requestAudit], has_more: false, page_size: 50 },
        },
      })
    })
    await page.route('**/api/audit/requests/*/attempts**', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            items: [
              {
                attempt_id: 'attempt-1',
                request_id: 'req-audit-1',
                attempt_no: 1,
                retry_index: 0,
                channel_id: 1,
                model: 'gpt-4o',
                fault_domain: '',
                request_type: 'chat',
                status: 'succeeded',
                success: true,
                status_code: 200,
                failure_class: '',
                stage: 'done',
                started_at: '2026-09-30T08:00:00Z',
                completed_at: '2026-09-30T08:00:01Z',
                duration_ms: 850,
              },
            ],
            has_more: false,
            page_size: 50,
          },
        },
      }),
    )
    await page.route('**/api/audit/events**', (route) =>
      route.fulfill({
        json: { success: true, data: { items: [], has_more: false, page_size: 50 } },
      }),
    )
    await page.route('**/api/audit/usage**', (route) => {
      if (route.request().url().includes('/stat')) return route.fallback()
      return route.fulfill({
        json: { success: true, data: { items: [], has_more: false, page_size: 50 } },
      })
    })
    await page.route('**/api/audit/usage/stat**', (route) =>
      route.fulfill({ json: { success: true, data: summary } }),
    )
    await page.goto('/audit')
    await expect(page.getByRole('heading', { name: '请求审计', exact: true })).toBeVisible()
    await page.getByRole('row').filter({ hasText: 'gpt-4o' }).click()
    await expect(page.getByRole('heading', { name: '请求详情', exact: true })).toBeVisible()
    await expect(page.getByText('req-audit-1').first()).toBeVisible()
    await expect(page.getByText('850 ms')).toBeVisible()
    await page.getByRole('button', { name: '关闭', exact: true }).click()
    await page.getByRole('tab', { name: '事件', exact: true }).click()
    await expect(page.getByText('暂无事件记录', { exact: true })).toBeVisible()
    await page.getByRole('tab', { name: '用量', exact: true }).click()
    await expect(page.getByText('暂无用量记录', { exact: true })).toBeVisible()
  })

  test('shows the empty state for requests', async ({ page }) => {
    await page.route('**/api/audit/requests**', (route) =>
      route.fulfill({
        json: { success: true, data: { items: [], has_more: false, page_size: 50 } },
      }),
    )
    await page.goto('/audit')
    await expect(page.getByText('暂无请求记录', { exact: true })).toBeVisible()
  })
})

test.describe('billing history', () => {
  test('renders the balance summary and ledger with signed amounts', async ({ page }) => {
    await page.route('**/api/billing/balance', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            account_id: 1,
            balance_micro: 5000000,
            version: 3,
            balance_micro_credits: '5000000',
          },
        },
      }),
    )
    await page.route('**/api/billing/entries**', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: {
            items: [
              {
                id: 1,
                amount_micro: -2000000,
                balance_after_micro: 5000000,
                kind: 'consume',
                operation_id: 'op-1',
                reason: 'usage',
                created_at: '2026-09-30T08:00:00Z',
              },
            ],
          },
        },
      }),
    )
    await page.goto('/billing')
    await expect(page.getByRole('heading', { name: '账单明细', exact: true })).toBeVisible()
    await expect(page.getByRole('definition').filter({ hasText: '5 credits' })).toBeVisible()
    await expect(page.getByRole('cell', { name: '-2 credits', exact: true })).toBeVisible()
  })

  test('shows the empty state when the ledger has no entries', async ({ page }) => {
    await page.route('**/api/billing/balance', (route) =>
      route.fulfill({
        json: {
          success: true,
          data: { account_id: 1, balance_micro: 0, version: 0, balance_micro_credits: '0' },
        },
      }),
    )
    await page.route('**/api/billing/entries**', (route) =>
      route.fulfill({ json: { success: true, data: { items: [] } } }),
    )
    await page.goto('/billing')
    await expect(page.getByText('暂无账本记录', { exact: true })).toBeVisible()
  })
})
