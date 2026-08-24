import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { classifyRequestHealth, getRequestHealthLabel } from './request-health'

describe('classifyRequestHealth', () => {
  test('uses the shared request-health thresholds', () => {
    assert.equal(classifyRequestHealth(100, 0), 'unknown')
    assert.equal(classifyRequestHealth(90, 1), 'healthy')
    assert.equal(classifyRequestHealth(89.99, 1), 'unstable')
    assert.equal(classifyRequestHealth(85, 1), 'unstable')
    assert.equal(classifyRequestHealth(84.99, 1), 'failed')
  })

  test('uses one label vocabulary across request-health surfaces', () => {
    assert.equal(getRequestHealthLabel('healthy'), '稳定')
    assert.equal(getRequestHealthLabel('unstable'), '波动')
    assert.equal(getRequestHealthLabel('failed'), '异常')
    assert.equal(getRequestHealthLabel('unknown'), '暂无近期请求')
  })
})
