import { describe, expect, it } from 'vitest'
import {
  multiplierText,
  notificationAction,
  unreadEvent,
  observedThroughID,
  exactCount,
} from './helpers'
import { notificationMessages } from './messages'

describe('notification transport and action boundaries', () => {
  it('validates a positive int64 list boundary without losing precision', () => {
    expect(observedThroughID('9007199254740993')).toBe('9007199254740993')
    expect(observedThroughID('9223372036854775807')).toBe('9223372036854775807')
    for (const value of ['0', '-1', '01', '', '9223372036854775808', 9007199254740993, undefined])
      expect(observedThroughID(value)).toBeUndefined()
  })
  it('preserves exact multiplier integers including values above the JS safe range', () => {
    expect(multiplierText('1234567')).toBe('1.234567')
    expect(multiplierText('1000000')).toBe('1')
    expect(multiplierText('9007199254740993')).toBe('9007199254.740993')
    expect(multiplierText('131145.14191981')).toBe('0.13114514191981')
    expect(multiplierText('1e-57')).toBe(`0.${'0'.repeat(62)}1`)
    expect(multiplierText('-100')).toBe('—')
    expect(multiplierText(1000000)).toBe('1')
  })
  it('accepts only valid unread counts from SSE, including zero', () => {
    expect(unreadEvent('{"unread_count":0}')).toBe(0n)
    expect(unreadEvent('{"unread_count":12}')).toBe(12n)
    expect(unreadEvent('{"unread_count":"5"}')).toBe(5n)
    expect(unreadEvent('{"unread_count":9007199254740993}')).toBe(9007199254740993n)
    for (const payload of [
      'null',
      'invalid',
      '{"unread_count":-1}',
      '{"unread_count":1.5}',
      '{"unread_count":"invalid"}',
      '{"unread_count":9223372036854775808}',
    ]) {
      expect(unreadEvent(payload)).toBeUndefined()
    }
  })
  it('keeps count values exact across generated bigint/string/number DTOs', () => {
    expect(exactCount('9007199254740993')).toBe(9007199254740993n)
    expect(exactCount(3n)).toBe(3n)
    expect(exactCount(0)).toBe(0n)
    for (const value of [-1n, 0.1, 9007199254740993, undefined, null, 'NaN'])
      expect(exactCount(value)).toBeUndefined()
  })
  it('does not turn server-provided notification targets into external or action links', () => {
    expect(notificationAction('/channel-market?group=9007199254740993')).toBe(
      '/channel-market?group=9007199254740993',
    )
    expect(notificationAction('/billing')).toBe('/billing')
    for (const url of [
      'https://ad.example',
      '//evil.example',
      '/api/user/logout',
      '/sign-in',
      '/%2f%2fevil.example',
      '/billing-history',
      '/billing\\evil',
    ])
      expect(notificationAction(url)).toBeUndefined()
  })
})

describe('notifications localization', () => {
  it('ships all nine languages with matching message placeholders', () => {
    for (const messages of Object.values(notificationMessages)) {
      expect(messages).toHaveLength(9)
      const placeholders = (messages[0].match(/\{\w+\}/g) ?? []).sort()
      for (const message of messages) {
        expect(message.trim()).not.toBe('')
        expect((message.match(/\{\w+\}/g) ?? []).sort()).toEqual(placeholders)
      }
    }
  })
})
