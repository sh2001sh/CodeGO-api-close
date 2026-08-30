import { AxiosError, AxiosHeaders } from 'axios'
import { describe, expect, it } from 'bun:test'
import { queryRetryDelay, shouldRetryQuery } from './query-retry'

function axiosError(status?: number, retryAfter?: string) {
  return new AxiosError(
    'request failed',
    undefined,
    undefined,
    undefined,
    status == null
      ? undefined
      : {
          status,
          statusText: '',
          config: { headers: new AxiosHeaders() },
          headers: new AxiosHeaders(
            retryAfter ? { 'retry-after': retryAfter } : undefined
          ),
          data: null,
        }
  )
}

describe('query retry policy', () => {
  it('does not retry permanent client errors', () => {
    expect(shouldRetryQuery(0, axiosError(400))).toBe(false)
    expect(shouldRetryQuery(0, axiosError(401))).toBe(false)
    expect(shouldRetryQuery(0, axiosError(403))).toBe(false)
    expect(shouldRetryQuery(0, axiosError(404))).toBe(false)
  })

  it('limits transient failures to two retries', () => {
    for (const status of [408, 429, 500, 503]) {
      expect(shouldRetryQuery(0, axiosError(status))).toBe(true)
      expect(shouldRetryQuery(1, axiosError(status))).toBe(true)
      expect(shouldRetryQuery(2, axiosError(status))).toBe(false)
    }
  })

  it('honors Retry-After and adds bounded jitter', () => {
    expect(queryRetryDelay(0, axiosError(429, '5'), () => 0)).toBe(5_000)
    expect(queryRetryDelay(0, axiosError(429, '5'), () => 1)).toBe(5_300)
    expect(queryRetryDelay(0, axiosError(429, '90'), () => 0)).toBe(30_000)
    expect(queryRetryDelay(0, axiosError(429, '90'), () => 1)).toBe(30_000)
  })

  it('uses exponential delay for other transient failures', () => {
    expect(queryRetryDelay(0, axiosError(503), () => 0)).toBe(600)
    expect(queryRetryDelay(2, axiosError(503), () => 0)).toBe(2_400)
  })
})
