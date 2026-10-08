import { expect, test } from '@playwright/test'
import { fixtureAPI } from './fixtures'
import { marketGroups } from './market-fixture'

test('the home market shows the legacy numeric public ID and carries it to the binding form', async ({
  page,
}) => {
  await fixtureAPI(page)
  await page.goto('/')
  await page.getByRole('button', { name: '公共模型渠道', exact: true }).click()
  const detail = page.locator('.board-detail')
  await expect(detail.getByText('349', { exact: true })).toBeVisible()
  await expect(detail.getByRole('button', { name: '复制分组 ID' })).toBeVisible()
  const bind = detail.getByRole('link', { name: '绑定此分组', exact: true })
  await expect(bind).toHaveAttribute('href', '/channel-market?group=%22349%22')
  await bind.click()
  await expect(page).toHaveURL(/\/channel-market\?group=%22349%22$/)
  await expect(page.locator('#market-group-group-test .market-listing-detail')).toBeVisible()
  await expect(
    page.locator('#market-group-group-test').getByRole('button', { name: '绑定 Key', exact: true }),
  ).toBeVisible()
})

test('an unquoted public ID beyond the safe integer range selects the exact group', async ({
  page,
}) => {
  await fixtureAPI(page)
  await marketGroups(page, [
    {
      id: '9007199254740993',
      group_id: 'group-test',
      system_display_name: '公共模型渠道',
      routing_group: 'market:group-test',
      declared_models: ['gpt-4o'],
      visibility: 'public',
      lifecycle_status: 'active',
      verification_status: 'passed',
      model_verification_results: [],
      model_prices: {},
      tags: ['openai'],
      multiplier: 1,
      multiplier_ppm: 1000000,
      recent_request_bucket_seconds: 3600,
      recent_request_series: [],
    },
  ])
  await page.goto('/channel-market?group=9007199254740993')
  await expect(page.locator('#market-group-group-test .market-listing-detail')).toBeVisible()
})
