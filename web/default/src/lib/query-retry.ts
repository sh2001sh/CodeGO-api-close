import axios from 'axios'

const TRANSIENT_CLIENT_STATUSES = new Set([408, 425, 429])
const MAX_RETRY_AFTER_MS = 30_000

function responseStatus(error: unknown): number | undefined {
  return axios.isAxiosError(error) ? error.response?.status : undefined
}

/** Limits automatic retries to failures that can reasonably recover. */
export function shouldRetryQuery(failureCount: number, error: unknown) {
  const status = responseStatus(error)
  if (status == null) return failureCount < 2
  if (status >= 400 && status < 500) {
    return TRANSIENT_CLIENT_STATUSES.has(status) && failureCount < 2
  }
  return status >= 500 && failureCount < 2
}

function retryAfterMilliseconds(error: unknown, now = Date.now()) {
  if (!axios.isAxiosError(error) || error.response?.status !== 429) return null
  const headers = error.response.headers
  const raw =
    typeof headers?.get === 'function'
      ? headers.get('retry-after')
      : headers?.['retry-after']
  if (raw == null || raw === '') return null

  const seconds = Number(raw)
  if (Number.isFinite(seconds)) {
    return Math.max(0, seconds * 1000)
  }
  const timestamp = Date.parse(String(raw))
  return Number.isNaN(timestamp) ? null : Math.max(0, timestamp - now)
}

/** Adds jitter so many tabs do not retry the same failed request together. */
export function queryRetryDelay(
  attemptIndex: number,
  error: unknown,
  random = Math.random
) {
  const retryAfter = retryAfterMilliseconds(error)
  const base =
    retryAfter == null
      ? Math.min(600 * 2 ** attemptIndex, 8_000)
      : Math.min(retryAfter, MAX_RETRY_AFTER_MS)
  const jitter = Math.floor(Math.max(0, Math.min(1, random())) * 300)
  return Math.min(base + jitter, MAX_RETRY_AFTER_MS)
}
