import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  SECTION_PAGE_CONTENT_CLASS_NAME,
  SECTION_PAGE_LAYOUT_CLASS_NAME,
} from './section-page-layout'

function hasClass(className: string, expectedClass: string) {
  return className.split(/\s+/).includes(expectedClass)
}

describe('SectionPageLayout scrolling contract', () => {
  test('constrains the page wrapper as a full-height column flex container', () => {
    for (const expectedClass of [
      'flex',
      'h-full',
      'min-h-0',
      'flex-1',
      'flex-col',
      'overflow-hidden',
    ]) {
      assert.equal(
        hasClass(SECTION_PAGE_LAYOUT_CLASS_NAME, expectedClass),
        true,
        `missing wrapper class: ${expectedClass}`
      )
    }
  })

  test('keeps page content as the only vertical scrolling region', () => {
    for (const expectedClass of ['min-h-0', 'flex-1', 'overflow-auto']) {
      assert.equal(
        hasClass(SECTION_PAGE_CONTENT_CLASS_NAME, expectedClass),
        true,
        `missing content class: ${expectedClass}`
      )
    }
  })
})
