import type { TFunction } from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { buildSidebarData } from './use-sidebar-data'

const translate = ((key: string) => key) as TFunction

describe('asset sidebar navigation', () => {
  test('keeps wallet, plans, and blind-box as direct asset entries', () => {
    const assets = buildSidebarData(translate).navGroups.find(
      (group) => group.id === 'assets'
    )

    assert.ok(assets)
    assert.equal(
      assets.items.some((item) => 'url' in item && item.url === '/wallet'),
      true
    )
    assert.equal(
      assets.items.some((item) => 'url' in item && item.url === '/blind-box'),
      true
    )
    assert.equal(
      assets.items.some((item) => 'url' in item && item.url === '/packages'),
      true
    )
  })
})
