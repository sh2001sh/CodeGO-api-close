import { test, expect, type Page } from '@playwright/test'
import { fixtureAPI } from './fixtures'

declare global {
  interface Window {
    notificationStreams: { emit: (type: string, data: unknown) => void; closed: boolean }[]
  }
}

async function notificationFixture(
  page: Page,
  options: { failRead?: boolean; failList?: boolean; many?: boolean; locale?: string } = {},
) {
  await fixtureAPI(page, options.locale ?? 'zh-CN')
  await page.addInitScript(() => {
    window.notificationStreams = []
    class FixtureEventSource extends EventTarget {
      onopen: ((event: Event) => void) | null = null
      onerror: ((event: Event) => void) | null = null
      closed = false
      constructor(_url: string) {
        super()
        window.notificationStreams.push(this)
        queueMicrotask(() => this.onopen?.(new Event('open')))
      }
      emit(type: string, data: unknown) {
        if (type === 'error') {
          this.onerror?.(new Event('error'))
          return
        }
        this.dispatchEvent(new MessageEvent(type, { data: JSON.stringify(data) }))
      }
      close() {
        this.closed = true
      }
    }
    Object.defineProperty(window, 'EventSource', { value: FixtureEventSource })
  })
  const notices = Array.from({ length: options.many ? 21 : 2 }, (_, index) => ({
    id: index === 0 ? '9007199254740993' : String(index + 1),
    category: index === 1 ? 'rewards' : 'market',
    kind: index === 1 ? 'lucky_reward' : 'multiplier_changed',
    title_key: index === 1 ? 'notifications.luckyReward' : 'notifications.multiplierChanged',
    body_key: index === 1 ? 'notifications.luckyRewardBody' : 'notifications.multiplierChangedBody',
    data:
      index === 1
        ? { reward_id: '5', final_reward_credits: '1234567' }
        : {
            channel_id: '9007199254740993',
            previous_multiplier_ppm: '1000000',
            multiplier_ppm: '800000',
            cleared: false,
          },
    action_url: index === 1 ? '/billing' : '/channel-market?group=9007199254740993',
    created_at: '2026-10-07T08:00:00Z',
    read_at: null as string | null,
  }))
  const calls: {
    path: string
    query: string
    method: string
    body?: { read?: boolean; through_id?: string; category?: string }
  }[] = []
  await page.route('**/api/notifications**', async (route) => {
    const url = new URL(route.request().url())
    const method = route.request().method()
    const body =
      method === 'POST'
        ? (route.request().postDataJSON() as {
            read?: boolean
            through_id?: string
            category?: string
          })
        : undefined
    calls.push({ path: url.pathname, query: url.search, method, body })
    const count = () => notices.filter((notice) => !notice.read_at).length
    const latestID = () =>
      notices.reduce(
        (latest, notice) => (BigInt(notice.id) > BigInt(latest) ? notice.id : latest),
        '0',
      )
    let data: unknown
    if (method === 'POST' && options.failRead) {
      await route.fulfill({ status: 403, json: { success: false, message: '无权执行此操作' } })
      return
    }
    if (url.pathname === '/api/notifications/summary')
      data = { unread_count: count(), latest_id: latestID() }
    else if (url.pathname === '/api/notifications/read-all') {
      const category = body?.category ?? 'all'
      for (const notice of notices)
        if (
          (category === 'all' || notice.category === category) &&
          body?.through_id &&
          BigInt(notice.id) <= BigInt(body.through_id)
        )
          notice.read_at = '2026-10-07T09:00:00Z'
      data = { read: true }
    } else if (method === 'POST') {
      const notice = notices.find(
        (notice) => url.pathname === `/api/notifications/${notice.id}/read`,
      )
      if (notice) notice.read_at = body?.read === false ? null : '2026-10-07T09:00:00Z'
      data = { read: true }
    } else if (url.pathname === '/api/notifications') {
      if (options.failList) {
        await route.fulfill({ status: 503, json: { success: false, message: '服务暂时不可用' } })
        return
      }
      const category = url.searchParams.get('category') ?? 'all'
      const filtered = notices.filter(
        (notice) =>
          (category === 'all' || notice.category === category) &&
          (url.searchParams.get('unread') !== 'true' || !notice.read_at),
      )
      const pageNumber = Number(url.searchParams.get('page') ?? 1)
      data = {
        items: filtered.slice((pageNumber - 1) * 20, pageNumber * 20),
        unread_count: count(),
        page: pageNumber,
        page_size: 20,
        total: filtered.length,
        latest_id: latestID(),
      }
    } else data = {}
    await route.fulfill({ json: { success: true, data } })
  })
  return {
    calls,
    addLater: () => notices.push({ ...notices[0], id: '9007199254740994', read_at: null }),
  }
}

test('bell, exact values, category read-all, and no notification polling', async ({
  page,
}, testInfo) => {
  const { calls } = await notificationFixture(page)
  await page.goto('/dashboard')
  const trigger = page.getByRole('button', { name: '消息通知，2 条未读', exact: true })
  await expect(trigger).toBeVisible()
  expect(calls.filter((call) => call.path === '/api/notifications')).toHaveLength(0)
  await trigger.click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByText('分组 9007199254740993 的倍率由 1× 调整为 0.8×。')).toBeVisible()
  await expect(drawer.getByText('奖励 1.234567 credits 已到账，可在账单明细查看。')).toBeVisible()
  await expect
    .poll(() => drawer.evaluate((element) => getComputedStyle(element).transform))
    .toBe('none')
  await page.screenshot({ path: testInfo.outputPath('notification-center.png') })
  await drawer.getByLabel('通知分类').selectOption('market')
  await drawer.getByRole('button', { name: '此分类全部标为已读', exact: true }).click()
  await expect(page.locator('.notification-trigger')).toHaveAttribute(
    'aria-label',
    '消息通知，1 条未读',
  )
  await drawer.getByRole('button', { name: '未读', exact: true }).click()
  await expect(drawer.getByText('没有未读通知', { exact: true })).toBeVisible()
  const before = calls.length
  await page.waitForTimeout(1600)
  expect(calls.length).toBe(before)
  await page.keyboard.press('Escape')
  await expect(drawer).not.toBeVisible()
  await expect(page.locator('.notification-trigger')).toBeFocused()
  expect(
    calls.some(
      (call) =>
        call.method === 'POST' &&
        call.path === '/api/notifications/read-all' &&
        call.body?.category === 'market' &&
        call.body.through_id === '9007199254740993',
    ),
  ).toBe(true)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('SSE updates the shared badge and paginated full page without duplicate streams', async ({
  page,
}) => {
  await notificationFixture(page, { many: true })
  await page.goto('/notifications')
  await expect(page.getByRole('heading', { name: '消息通知', level: 1 })).toBeVisible()
  await expect(page.locator('.notification-list > li')).toHaveCount(20)
  expect(
    await page.evaluate(() => window.notificationStreams.filter((stream) => !stream.closed).length),
  ).toBe(1)
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.locator('.notification-list > li')).toHaveCount(1)
  await page.evaluate(() =>
    window.notificationStreams
      .find((stream) => !stream.closed)
      ?.emit('invalidate', { unread_count: 25 }),
  )
  await expect(page.locator('.notification-trigger')).toHaveAttribute(
    'aria-label',
    '消息通知，25 条未读',
  )
  await page.evaluate(() =>
    window.notificationStreams.find((stream) => !stream.closed)?.emit('error', {}),
  )
  await expect(page.getByText('实时通知连接中断。重试或返回此页面时会重新连接。')).toBeVisible()
  expect(
    await page.evaluate(() => window.notificationStreams.filter((stream) => !stream.closed).length),
  ).toBe(0)
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect
    .poll(() =>
      page.evaluate(() => window.notificationStreams.filter((stream) => !stream.closed).length),
    )
    .toBe(1)
  await page.evaluate(() =>
    window.notificationStreams
      .find((stream) => !stream.closed)
      ?.emit('unread_count', { unread_count: 25 }),
  )
  await expect(page.getByText('实时通知连接中断。重试或返回此页面时会重新连接。')).toHaveCount(0)
})

test('a failed read keeps the notice unread and reports the error', async ({ page }) => {
  const { calls } = await notificationFixture(page, { failRead: true })
  await page.goto('/notifications')
  await expect(page.locator('.notification-list > li')).toHaveCount(2)
  await page.getByRole('button', { name: '标为已读', exact: true }).first().click()
  await expect(page.getByRole('alert')).toContainText('无权执行此操作')
  await expect(page.locator('.notification-trigger')).toHaveAttribute(
    'aria-label',
    '消息通知，2 条未读',
  )
  await expect(page.locator('.notification-list > li[data-unread]')).toHaveCount(2)
  expect(calls.some((call) => call.path === '/api/notifications/9007199254740993/read')).toBe(true)
  await page.getByRole('link', { name: '查看详情', exact: true }).first().click()
  await expect
    .poll(
      () => calls.filter((call) => call.path === '/api/notifications/9007199254740993/read').length,
    )
    .toBe(2)
  await expect(page).toHaveURL(/\/notifications$/)
  await expect(page.getByRole('alert')).toContainText('无权执行此操作')
})

test('notification list errors expose a retry action', async ({ page }) => {
  await notificationFixture(page, { failList: true })
  await page.goto('/notifications')
  await expect(page.getByRole('alert')).toContainText('通知加载失败，请重试。')
  await expect(page.getByRole('button', { name: '重试', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '全部标为已读', exact: true })).toBeDisabled()
})

test('read-all uses the list snapshot and preserves a later unseen message', async ({ page }) => {
  const { calls, addLater } = await notificationFixture(page)
  await page.goto('/notifications')
  await expect(page.locator('.notification-list > li')).toHaveCount(2)
  addLater()
  // Opening the drawer advances the live summary, while the fresh cached list remains observed.
  await page.locator('.notification-trigger').click()
  const drawer = page.getByRole('dialog')
  await expect(page.locator('.notification-trigger')).toHaveAttribute(
    'aria-label',
    '消息通知，3 条未读',
  )
  await expect(drawer.locator('.notification-list > li')).toHaveCount(2)
  await drawer.getByRole('button', { name: '全部标为已读', exact: true }).click()
  await expect(page.locator('.notification-trigger')).toHaveAttribute(
    'aria-label',
    '消息通知，1 条未读',
  )
  expect(calls.find((call) => call.path === '/api/notifications/read-all')?.body).toEqual({
    through_id: '9007199254740993',
    category: 'all',
  })
  await expect(drawer.locator('.notification-list > li[data-unread]')).toHaveCount(1)
})

test('read messages can be marked unread and focus reconciles without polling', async ({
  page,
}) => {
  const { calls, addLater } = await notificationFixture(page)
  await page.goto('/notifications')
  await expect(page.locator('.notification-list > li')).toHaveCount(2)
  await page.getByRole('button', { name: '标为已读', exact: true }).first().click()
  await expect(page.getByRole('button', { name: '标为未读', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '标为未读', exact: true }).click()
  await expect(page.locator('.notification-trigger')).toHaveAttribute(
    'aria-label',
    '消息通知，2 条未读',
  )
  expect(
    calls
      .filter((call) => call.path === '/api/notifications/9007199254740993/read')
      .map((call) => call.body),
  ).toEqual([{ read: true }, { read: false }])
  addLater()
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect(page.locator('.notification-trigger')).toHaveAttribute(
    'aria-label',
    '消息通知，3 条未读',
  )
  await expect(page.locator('.notification-list > li')).toHaveCount(3)
  const afterFocus = calls.length
  await page.waitForTimeout(1300)
  expect(calls.length).toBe(afterFocus)
})

test('Arabic notifications use the existing RTL drawer rules', async ({ page }) => {
  await notificationFixture(page, { locale: 'ar' })
  await page.goto('/dashboard')
  await page.locator('.notification-trigger').click()
  const drawer = page.getByRole('dialog')
  await expect(drawer.getByRole('heading', { name: 'الإشعارات' })).toBeVisible()
  await expect(drawer.getByRole('combobox')).toBeVisible()
  await expect.poll(async () => (await drawer.boundingBox())?.x).toBe(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('SSE reconnects after a transient drop and cancels retries while hidden', async ({ page }) => {
  const { calls } = await notificationFixture(page)
  await page.clock.install()
  await page.goto('/notifications')
  await expect(page.locator('.notification-list > li')).toHaveCount(2)
  await expect.poll(() => page.evaluate(() => window.notificationStreams.length)).toBe(1)
  await page.evaluate(() => window.notificationStreams[0].emit('error', {}))
  await page.clock.fastForward(1000)
  await expect.poll(() => page.evaluate(() => window.notificationStreams.length)).toBe(2)
  expect(
    await page.evaluate(() => window.notificationStreams.filter((stream) => !stream.closed).length),
  ).toBe(1)
  await page.evaluate(() => {
    window.notificationStreams[1].emit('error', {})
    Object.defineProperty(document, 'hidden', { configurable: true, value: true })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  const beforeHidden = calls.length
  await page.clock.fastForward(40_000)
  expect(await page.evaluate(() => window.notificationStreams.length)).toBe(2)
  expect(calls.length).toBe(beforeHidden)
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect.poll(() => page.evaluate(() => window.notificationStreams.length)).toBe(3)
  const reconnected = calls.length
  await page.clock.fastForward(40_000)
  expect(calls.length).toBe(reconnected)
})

test('reconnect waits for the expired session refresh before creating its stream', async ({
  page,
}) => {
  await notificationFixture(page)
  let expired = false
  let refreshing = false
  let releaseRefresh: () => void = () => {}
  const refreshGate = new Promise<void>((resolve) => {
    releaseRefresh = resolve
  })
  await page.route('**/api/notifications/summary', async (route) => {
    if (expired) {
      await route.fulfill({ status: 401, json: { success: false, message: '请先登录' } })
      return
    }
    await route.fulfill({
      json: { success: true, data: { unread_count: 2, latest_id: '9007199254740993' } },
    })
  })
  await page.route('**/api/user/refresh', async (route) => {
    refreshing = true
    await refreshGate
    expired = false
    await route.fulfill({ json: { success: true, data: {} } })
  })
  await page.goto('/notifications')
  await expect.poll(() => page.evaluate(() => window.notificationStreams.length)).toBe(1)
  expired = true
  await page.evaluate(() => window.notificationStreams[0].emit('error', {}))
  await expect.poll(() => refreshing).toBe(true)
  expect(await page.evaluate(() => window.notificationStreams.length)).toBe(1)
  expect(
    await page.evaluate(() => window.notificationStreams.filter((stream) => !stream.closed).length),
  ).toBe(0)
  releaseRefresh()
  await expect.poll(() => page.evaluate(() => window.notificationStreams.length)).toBe(2)
  expect(
    await page.evaluate(() => window.notificationStreams.filter((stream) => !stream.closed).length),
  ).toBe(1)
})

test('initial summary precedes SSE and a delayed summary cannot replace a newer event', async ({
  page,
}) => {
  await notificationFixture(page)
  const releases: (() => void)[] = []
  await page.route('**/api/notifications/summary', async (route) => {
    await new Promise<void>((resolve) => releases.push(resolve))
    await route.fulfill({
      json: { success: true, data: { unread_count: 2, latest_id: '9007199254740993' } },
    })
  })
  await page.goto('/dashboard')
  await expect.poll(() => releases.length).toBe(1)
  expect(await page.evaluate(() => window.notificationStreams.length)).toBe(0)
  releases[0]()
  await expect(page.locator('.notification-trigger')).toHaveAttribute(
    'aria-label',
    '消息通知，2 条未读',
  )
  await expect.poll(() => page.evaluate(() => window.notificationStreams.length)).toBe(1)
  await page.evaluate(() => window.dispatchEvent(new Event('focus')))
  await expect.poll(() => releases.length).toBe(2)
  await page.evaluate(() =>
    window.notificationStreams[0].emit('invalidate', {
      unread_count: '9007199254740994',
      latest_id: '9007199254740994',
    }),
  )
  await expect(page.locator('.notification-trigger')).toHaveAttribute(
    'aria-label',
    '消息通知，9007199254740994 条未读',
  )
  releases[1]()
  await page.waitForTimeout(200)
  await expect(page.locator('.notification-trigger')).toHaveAttribute(
    'aria-label',
    '消息通知，9007199254740994 条未读',
  )
})
