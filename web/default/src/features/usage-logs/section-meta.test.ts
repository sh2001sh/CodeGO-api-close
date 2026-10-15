import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  getUsageLogsSectionMeta,
  resolveUsageLogsSectionId,
  resolveUsageLogsRouteRedirect,
} from './section-meta.ts'

describe('usage logs section meta', () => {
  test('returns the correct copy for each section', () => {
    assert.equal(getUsageLogsSectionMeta('common').titleKey, 'Common Logs')
    assert.equal(getUsageLogsSectionMeta('task').titleKey, 'Task Logs')
  })
})

describe('usage logs route redirect resolution', () => {
  test('normalizes arbitrary section ids to the default section', () => {
    assert.equal(resolveUsageLogsSectionId('logs'), 'common')
    assert.equal(resolveUsageLogsSectionId('task'), 'task')
  })

  test('redirects unknown sections to the default section', () => {
    assert.deepEqual(resolveUsageLogsRouteRedirect('logs'), {
      section: 'common',
    })
  })

  test('keeps common searches intact', () => {
    assert.equal(resolveUsageLogsRouteRedirect('common', { type: ['1'] }), null)
  })

  test('drops type filters for non-common sections before navigation', () => {
    assert.deepEqual(
      resolveUsageLogsRouteRedirect('task', {
        type: ['1'],
        page: 2,
      }),
      {
        section: 'task',
        search: {
          type: undefined,
          page: 2,
        },
        replace: true,
      }
    )
  })
})
