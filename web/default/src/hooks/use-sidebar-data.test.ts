import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { TFunction } from 'i18next'
import { buildSidebarData } from './use-sidebar-data'

const translate = ((key: string) => key) as TFunction

describe('personal sidebar navigation', () => {
  test('keeps blind-box and plans as direct entries', () => {
    const personal = buildSidebarData(translate).navGroups.find(
      (group) => group.id === 'personal'
    )

    assert.ok(personal)
    assert.equal(
      personal.items.some(
        (item) => 'url' in item && item.url === '/blind-box'
      ),
      true
    )
    assert.equal(
      personal.items.some((item) => 'url' in item && item.url === '/packages'),
      true
    )

    const benefits = personal.items.find(
      (item) => 'items' in item && item.title === '额度与权益'
    )
    if (!benefits || !('items' in benefits) || !benefits.items) {
      assert.fail('missing benefits collapsible navigation')
    }
    assert.equal(
      benefits.items.some(
        (item) => item.url === '/blind-box' || item.url === '/packages'
      ),
      false
    )
  })
})
