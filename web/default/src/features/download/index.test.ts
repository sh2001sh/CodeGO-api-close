import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { detectDesktopPlatform } from './lib.ts'

describe('download feature surface', () => {
  test('desktop recommendation falls back to unknown for non-browser callers', () => {
    assert.equal(detectDesktopPlatform(''), 'unknown')
  })
})
