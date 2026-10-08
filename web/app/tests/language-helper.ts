import type { Page } from '@playwright/test'

export async function selectLanguage(page: Page, name: string) {
  await page.getByRole('button', { name: /\/ Language$/ }).click()
  await page.getByRole('menuitemradio', { name, exact: true }).click()
}
