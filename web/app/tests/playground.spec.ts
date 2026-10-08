import { test, expect, type Page } from '@playwright/test'
import { fixtureAPI } from './fixtures'

test.beforeEach(async ({ page }) => fixtureAPI(page))

async function mockKeySecret(page: Page, key = 'sk-test-secret') {
  await page.route('**/api/user/self/groups', (route) =>
    route.fulfill({ json: { success: true, data: ['default', 'premium'] } }),
  )
  await page.route('**/api/token/*/key', (route) =>
    route.fulfill({ json: { success: true, data: { key } } }),
  )
  await page.route('**/v1/models', (route) =>
    route.fulfill({ json: { success: true, object: 'list', data: [{ id: 'gpt-4o-mini' }] } }),
  )
}

async function useFirstKey(page: Page) {
  await page.goto('/playground')
  await page.getByLabel('API Key', { exact: true }).selectOption('1')
  await page.getByRole('button', { name: '使用此 Key', exact: true }).click()
  await expect(page.getByLabel('模型', { exact: true })).toHaveValue('gpt-4o-mini')
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeEnabled()
}

test('streams assistant content chunk by chunk', async ({ page }) => {
  await mockKeySecret(page)
  await page.route('**/v1/chat/completions', (route) =>
    route.fulfill({
      contentType: 'text/event-stream',
      body:
        'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n' +
        'data: {"choices":[{"delta":{"content":" world"}}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}\n\n' +
        'data: [DONE]\n\n',
    }),
  )
  await useFirstKey(page)
  await page.getByLabel('消息输入', { exact: true }).fill('Hi there')
  await page.getByLabel('消息输入', { exact: true }).press('Enter')
  await expect(page.getByText('Hello world', { exact: true })).toBeVisible()
  await expect(page.getByText('用量', { exact: false })).toContainText('总计')
})

test('stop aborts an in-flight stream without raising an error', async ({ page }) => {
  await mockKeySecret(page)
  await page.route('**/v1/chat/completions', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 3000))
    await route.fulfill({ contentType: 'text/event-stream', body: 'data: [DONE]\n\n' })
  })
  await useFirstKey(page)
  await page.getByLabel('消息输入', { exact: true }).fill('Long running')
  await page.getByLabel('消息输入', { exact: true }).press('Enter')
  await expect(page.getByRole('button', { name: '停止', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '停止', exact: true }).click()
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('insufficient credits shows the gateway error with guidance', async ({ page }) => {
  await mockKeySecret(page)
  await page.route('**/v1/chat/completions', (route) =>
    route.fulfill({ status: 402, json: { error: { message: 'not enough credits' } } }),
  )
  await useFirstKey(page)
  await page.getByLabel('消息输入', { exact: true }).fill('Hi there')
  await page.getByLabel('消息输入', { exact: true }).press('Enter')
  await expect(page.getByRole('alert').first()).toHaveText('not enough credits')
  await expect(page.getByText('余额不足，请前往钱包充值后重试。')).toBeVisible()
})

test('"查看代码" shows a curl sample for the current request', async ({ page }) => {
  await mockKeySecret(page)
  await page.route('**/v1/chat/completions', (route) =>
    route.fulfill({ contentType: 'text/event-stream', body: 'data: [DONE]\n\n' }),
  )
  await useFirstKey(page)
  await page.getByLabel('消息输入', { exact: true }).fill('Hi there')
  await page.getByLabel('消息输入', { exact: true }).press('Enter')
  await page.getByRole('button', { name: '查看代码', exact: true }).click()
  await expect(page.getByText('curl ', { exact: false }).first()).toBeVisible()
})

test('group selection changes model discovery and the actual streamed route without rebinding a key', async ({
  page,
}) => {
  await mockKeySecret(page)
  await page.route('**/v1/models', (route) =>
    route.fulfill({
      json: {
        data: [
          {
            id:
              route.request().headers()['x-codego-group'] === 'premium'
                ? 'claude-sonnet-4'
                : 'gpt-4o-mini',
          },
        ],
      },
    }),
  )
  let sentGroup = ''
  let sentModel = ''
  let keyMutations = 0
  page.on('request', (request) => {
    if (request.method() === 'PUT' && request.url().includes('/api/token/')) keyMutations += 1
    if (request.url().includes('/bind-token')) keyMutations += 1
  })
  await page.route('**/v1/chat/completions', (route) => {
    sentGroup = route.request().headers()['x-codego-group']
    sentModel = route.request().postDataJSON().model
    return route.fulfill({
      contentType: 'text/event-stream',
      body: 'data: {"choices":[{"delta":{"content":"已使用所选分组"}}]}\n\ndata: [DONE]\n\n',
    })
  })
  await useFirstKey(page)
  await page.getByLabel('分组', { exact: true }).selectOption('premium')
  await expect(page.getByLabel('模型', { exact: true })).toHaveValue('claude-sonnet-4')
  await page.getByLabel('消息输入', { exact: true }).fill('使用这个模型')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByText('已使用所选分组', { exact: true })).toBeVisible()
  expect(sentGroup).toBe('premium')
  expect(sentModel).toBe('claude-sonnet-4')
  expect(keyMutations).toBe(0)
  await page.getByRole('button', { name: '查看代码', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('X-CodeGo-Group: premium')
})

test('conversation labels use lean routing candidates without loading full market statistics', async ({
  page,
}) => {
  await mockKeySecret(page)
  let fullMarketReads = 0
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname
    if (path === '/api/marketplace/groups' || path === '/api/marketplace/key-group-options')
      fullMarketReads += 1
  })
  await page.route('**/api/marketplace/route-pools/group-options', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: [
          {
            group_id: 'official:default',
            routing_group: 'default',
            name: '官方分组',
            display_id: 'default',
            kind: 'official',
            multiplier: 1,
            models: ['gpt-4o-mini'],
          },
          {
            group_id: 'internal-market-id',
            routing_group: 'premium',
            name: '我的渠道',
            display_id: '9007199254740993',
            kind: 'market',
            multiplier: 1,
            models: ['gpt-4o-mini'],
          },
          {
            group_id: 'inaccessible-candidate',
            routing_group: 'not-in-allowed-groups',
            name: '未授权分组',
            display_id: '9',
            kind: 'market',
            multiplier: 1,
            models: ['gpt-4o-mini'],
          },
        ],
      },
    }),
  )
  await useFirstKey(page)
  const groups = page.getByLabel('分组', { exact: true })
  await expect(groups.locator('option[value="default"]')).toHaveText('官方分组')
  await expect(groups.locator('option[value="premium"]')).toHaveText('我的渠道 · 9007199254740993')
  await expect(groups.locator('option[value="not-in-allowed-groups"]')).toHaveCount(0)
  await groups.selectOption('premium')
  await expect(groups).toHaveValue('premium')
  expect(fullMarketReads).toBe(0)
})

test('routing label retrieval failure is displayed and never falls back to the full market list', async ({
  page,
}) => {
  await mockKeySecret(page)
  let fullMarketReads = 0
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/marketplace/key-group-options')
      fullMarketReads += 1
  })
  await page.route('**/api/marketplace/route-pools/group-options', (route) =>
    route.fulfill({
      status: 503,
      json: { success: false, message: '分组名称暂不可用' },
    }),
  )
  await page.goto('/playground')
  await expect(page.getByRole('alert')).toContainText('分组名称暂不可用')
  await expect(
    page.getByLabel('分组', { exact: true }).locator('option[value="premium"]'),
  ).toHaveText('premium')
  expect(fullMarketReads).toBe(0)
})

test('a revoked group is rejected and displayed without a fabricated answer', async ({ page }) => {
  await mockKeySecret(page)
  await page.route('**/v1/chat/completions', (route) =>
    route.fulfill({
      status: 403,
      json: { error: { code: 'group_not_allowed', message: 'API key does not allow this group' } },
    }),
  )
  await useFirstKey(page)
  await page.getByLabel('分组', { exact: true }).selectOption('premium')
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeEnabled()
  await page.getByLabel('消息输入', { exact: true }).fill('测试已撤销权限')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('alert').first()).toHaveText('API key does not allow this group')
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeEnabled()
})

test('conversation has Chinese settings and cannot send before selecting a usable key and model', async ({
  page,
}) => {
  await mockKeySecret(page)
  await page.goto('/playground')
  await expect(page.getByRole('heading', { name: '对话', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '发送', exact: true })).toBeDisabled()
  await expect(page.getByRole('heading', { name: '今天想聊些什么？', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '对话设置', exact: true }).click()
  await expect(page.getByLabel('温度', { exact: true })).toBeVisible()
  await expect(page.getByLabel('最大输出长度', { exact: true })).toBeVisible()
  await expect(page.getByLabel('系统提示词', { exact: true })).toBeVisible()
  const noOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth <= window.innerWidth,
  )
  expect(noOverflow).toBe(true)
})

test('batch actions enable, disable and report partial failure', async ({ page }) => {
  await page.route('**/api/token/', async (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    await route.fulfill({
      json: {
        success: true,
        data: [
          {
            id: 1,
            name: '密钥一',
            key_prefix: 'sk-one',
            status: 'active',
            allowed_models: [],
            allowed_cidrs: [],
            group: null,
            expires_at: null,
            created_at: '2026-09-30T08:00:00Z',
            last_used_at: null,
          },
          {
            id: 2,
            name: '密钥二',
            key_prefix: 'sk-two',
            status: 'active',
            allowed_models: [],
            allowed_cidrs: [],
            group: null,
            expires_at: null,
            created_at: '2026-09-30T08:00:00Z',
            last_used_at: null,
          },
        ],
      },
    })
  })
  let putCount = 0
  await page.route('**/api/token/*', async (route) => {
    if (route.request().method() !== 'PUT') return route.fallback()
    putCount += 1
    if (putCount === 2)
      return route.fulfill({ status: 500, json: { success: false, message: '服务暂时不可用' } })
    await route.fulfill({ json: { success: true, data: null } })
  })
  await page.goto('/keys')
  await page.getByRole('checkbox', { name: '选择 密钥一' }).check()
  await page.getByRole('checkbox', { name: '选择 密钥二' }).check()
  await page.getByRole('button', { name: '批量停用', exact: true }).click()
  await expect(page.getByText('1 / 2 项操作失败，请检查后重试')).toBeVisible()
})

test('客户端配置 dialog reveals the secret and renders copyable snippets', async ({ page }) => {
  await mockKeySecret(page, 'sk-config-secret')
  await page.goto('/keys')
  await page.getByRole('button', { name: '客户端配置', exact: true }).first().click()
  await expect(page.getByText('sk-config-secret', { exact: false }).first()).toBeVisible()
  await expect(page.getByText('OPENAI_API_KEY', { exact: false }).first()).toBeVisible()
})

test('invalid output settings keep the draft and do not send or create a response', async ({
  page,
}) => {
  await mockKeySecret(page)
  let calls = 0
  await page.route('**/v1/chat/completions', (route) => {
    calls += 1
    return route.fulfill({ contentType: 'text/event-stream', body: 'data: [DONE]\n\n' })
  })
  await useFirstKey(page)
  await page.getByRole('button', { name: '对话设置', exact: true }).click()
  await page.getByLabel('最大输出长度', { exact: true }).fill('-1')
  await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
  await page.getByLabel('消息输入', { exact: true }).fill('保留我的草稿')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('最大输出长度须为正整数')
  await expect(page.getByLabel('消息输入', { exact: true })).toHaveValue('保留我的草稿')
  await expect(page.getByRole('log')).toHaveCount(0)
  expect(calls).toBe(0)
})

test('input during streaming is preserved and composition Enter never sends', async ({ page }) => {
  await mockKeySecret(page)
  await page.route('**/v1/chat/completions', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 3000))
    await route.fulfill({ contentType: 'text/event-stream', body: 'data: [DONE]\n\n' })
  })
  await useFirstKey(page)
  const input = page.getByLabel('消息输入', { exact: true })
  await input.fill('中文输入中')
  await input.dispatchEvent('keydown', { key: 'Enter', isComposing: true })
  await expect(input).toHaveValue('中文输入中')
  await expect(page.getByRole('button', { name: '停止', exact: true })).toHaveCount(0)
  await input.press('Enter')
  await expect(page.getByRole('button', { name: '停止', exact: true })).toBeVisible()
  await input.fill('下一条消息')
  await input.press('Enter')
  await expect(input).toHaveValue('下一条消息')
  await page.getByRole('button', { name: '停止', exact: true }).click()
  await expect(input).toHaveValue('下一条消息')
})
