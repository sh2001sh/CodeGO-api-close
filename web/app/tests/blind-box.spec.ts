import { test, expect } from '@playwright/test'
import { fixtureAPI } from './fixtures'

const pool = {
  id: 1,
  name: '透明奖池',
  enabled: true,
  price_micro: 1000000,
  daily_limit: 5,
  monthly_limit: 10,
  daily_open_limit: 5,
  scope: 'credits',
  guarantees: {},
  standard_policy: { enabled: false },
  rewards: [1, 2, 3, 4, 5].map((id) => ({
    kind: 'credits',
    title: `奖励 ${id}`,
    weight: 1,
    amount_micro: 1000000,
  })),
}
const multiplier = {
  id: '9007199254740993',
  kind: 'multiplier',
  title: '历史倍率卡',
  status: 'available',
  multiplier_ppm: 500000,
  remaining_seconds: 3600,
  max_discount_micro: 0,
  discount_rate_ppm: 0,
  prop_type: '',
}
const coupon = {
  ...multiplier,
  id: 2,
  kind: 'topup_discount',
  title: '历史充值券',
  prop_type: 'topup_discount_80',
  discount_rate_ppm: 800000,
}

test.beforeEach(async ({ page }) => fixtureAPI(page))

test('shows full-pool weights even in compact preview and supports mobile layout', async ({
  page,
}, info) => {
  await page.route('**/api/blind-box/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { available_count: 2, pools: [pool], props: [multiplier, coupon], pity_states: {} },
      },
    }),
  )
  await page.goto('/blind-box')
  await expect(page.getByRole('heading', { name: '基础奖池占比' })).toBeVisible()
  await expect(page.locator('.box-probability:visible')).toHaveText([
    '20.00%',
    '20.00%',
    '20.00%',
    '20.00%',
  ])
  await page.getByText('查看其余基础奖励', { exact: true }).click()
  await expect(page.getByText('奖励 5', { exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: '历史倍率卡', exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
  await page.evaluate(() => window.scrollTo(0, 0))
  await expect(page.getByRole('heading', { name: '盲盒', exact: true })).toBeInViewport()
  await page.screenshot({ path: `../../output/blind-box-${info.project.name}.png`, fullPage: true })
})

test('retries a failed purchase with the same ID and separates opening identity', async ({
  page,
}) => {
  await page.route('**/api/blind-box/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: { available_count: 2, pools: [pool], props: [], pity_states: {} },
      },
    }),
  )
  const purchases: { request_id: string }[] = []
  await page.route('**/api/blind-box/inventory/purchase', async (route) => {
    purchases.push(route.request().postDataJSON())
    await route.fulfill(
      purchases.length === 1
        ? { status: 503, json: { success: false, message: '暂时不可用' } }
        : { json: { success: true, data: { id: 1, quantity: 1, total_micro: 1000000 } } },
    )
  })
  await page.route('**/api/blind-box/inventory/open', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [{ id: 1, reward: { kind: 'credits', title: '已到账奖励', amount_micro: 1000000 } }],
      },
    }),
  )
  await page.goto('/blind-box')
  for (let index = 0; index < 2; index++) {
    await page.getByRole('button', { name: '购买一个', exact: true }).click()
    await page.getByRole('dialog').getByRole('button', { name: '确认', exact: true }).click()
    if (index === 0) await expect(page.getByRole('alert')).toHaveText('暂时不可用')
    else await expect(page.getByRole('status')).toContainText('购买成功')
  }
  expect(purchases[0].request_id).toBe(purchases[1].request_id)
  const opening = page.waitForRequest('**/api/blind-box/inventory/open')
  await page.getByRole('button', { name: '开启一个', exact: true }).click()
  expect((await opening).postDataJSON().request_id).not.toBe(purchases[0].request_id)
  await expect(page.getByRole('heading', { name: '本次开启结果' })).toBeVisible()
})

test('disabled pools do not prevent old inventory or retained-card use, pause, convert and transfer', async ({
  page,
}) => {
  await page.route('**/api/blind-box/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          available_count: 2,
          pools: [{ ...pool, enabled: false }],
          props: [
            multiplier,
            { ...multiplier, id: 3, title: '使用中的旧卡', status: 'active' },
            coupon,
          ],
          pity_states: {},
        },
      },
    }),
  )
  await page.route('**/api/blind-box/props/*/*', (route) =>
    route.fulfill({ json: { success: true, data: {} } }),
  )
  await page.goto('/blind-box')
  await expect(page.getByText('暂无可购买盲盒', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '购买一个', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '开启一个', exact: true })).toBeEnabled()
  const use = page.waitForRequest('**/api/blind-box/props/9007199254740993/use')
  await page.getByRole('button', { name: '使用', exact: true }).click()
  await use
  const pause = page.waitForRequest('**/api/blind-box/props/3/pause')
  await page.getByRole('button', { name: '暂停', exact: true }).click()
  await pause
  const convert = page.waitForRequest('**/api/blind-box/props/2/convert')
  await page.getByRole('button', { name: '转换为九折充值卡', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '确认', exact: true }).click()
  expect((await convert).postDataJSON()).toEqual({ target_type: 'topup_discount_90' })
  await page.getByText('赠送盲盒与道具', { exact: true }).click()
  await page.getByLabel('道具', { exact: true }).selectOption('9007199254740993')
  await page.getByLabel('道具收件人用户 ID', { exact: true }).fill('9007199254740993')
  const gift = page.waitForRequest('**/api/blind-box/props/9007199254740993/gift')
  await page.getByRole('button', { name: '赠送', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('9007199254740993')
  await page.getByRole('dialog').getByRole('button', { name: '确认', exact: true }).click()
  expect((await gift).postData()).toContain('"recipient_id":9007199254740993')
})

test('invalid weights cannot be bought and invalid recipients cannot be submitted', async ({
  page,
}) => {
  let gifts = 0
  await page.route('**/api/blind-box/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          available_count: 2,
          pools: [
            {
              ...pool,
              rewards: [{ kind: 'credits', title: '错误权重', weight: 0, amount_micro: 1 }],
            },
          ],
          props: [],
          pity_states: {},
        },
      },
    }),
  )
  await page.route('**/api/blind-box/inventory/gift', (route) => {
    gifts++
    return route.fulfill({ json: { success: true, data: [] } })
  })
  await page.goto('/blind-box')
  await expect(page.getByRole('button', { name: '购买一个', exact: true })).toBeDisabled()
  await expect(page.getByText('概率暂不可用', { exact: true })).toBeVisible()
  await page.getByText('赠送盲盒与道具', { exact: true }).click()
  await page.getByLabel('收件人用户 ID', { exact: true }).fill('1e3')
  await page.getByRole('button', { name: '赠送一个', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('请输入有效的收件人用户 ID')
  expect(gifts).toBe(0)
})

test('large weights remain exact in RTL and the purchase review is keyboard accessible', async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem('codego.locale', 'ar'))
  await page.route('**/api/blind-box/self', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          available_count: 0,
          pools: [
            {
              ...pool,
              rewards: [
                {
                  kind: 'credits',
                  title: 'Large weight',
                  weight: '9007199254740993',
                  amount_micro: 1,
                },
                { kind: 'credits', title: 'Small weight', weight: 1, amount_micro: 2 },
              ],
            },
          ],
          props: [],
          pity_states: {},
        },
      },
    }),
  )
  await page.goto('/blind-box')
  await expect(page.locator('html')).toHaveAttribute('dir', 'rtl')
  await expect(page.locator('.box-probability')).toHaveText(['99.99%', '<0.01%'])
  await page.locator('.box-pool-order button').focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(page.locator('.box-pool-order button')).toBeFocused()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  )
})
