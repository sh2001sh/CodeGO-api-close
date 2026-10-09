import createClient from 'openapi-fetch'
import type { MaybeOptionalInit, FetchResponse } from 'openapi-fetch'
import type { PathsWithMethod, RequiredKeysOf } from 'openapi-typescript-helpers'
import { isLosslessNumber, parse, stringify } from 'lossless-json'
import type { paths } from './api.generated'
import { integerRequestFields } from './api.generated'

export class APIError extends Error {
  constructor(
    message: string,
    public readonly status: number,
  ) {
    super(message)
  }
}

let refreshing: Promise<boolean> | undefined

function decodeJSON(text: string, marketplaceFactors = false): unknown {
  return parse(text, (key, value) => {
    if (!isLosslessNumber(value)) return value
    const raw = value.value
    // Fractional scaled PPM and historical multipliers must never pass through
    // Number. Other API decimals (scores, probabilities and charts) stay numeric.
    const exactFactor =
      ['multiplier_ppm', 'previous_multiplier_ppm', 'proposed_ppm'].includes(key) ||
      (marketplaceFactors && ['multiplier', 'proposed_multiplier'].includes(key))
    if (exactFactor && !/^-?\d+$/.test(raw)) return raw
    const number = Number(raw)
    return Number.isInteger(number) && !Number.isSafeInteger(number) ? raw : number
  })
}

function integerField(value: unknown, path: readonly string[]): unknown {
  if (!path.length) {
    if (value === null || value === undefined) return value
    if (typeof value !== 'bigint' && typeof value !== 'number' && typeof value !== 'string')
      throw new APIError('整数格式无效', 400)
    if (typeof value === 'number' && !Number.isSafeInteger(value))
      throw new APIError('整数超出安全范围，请使用精确字符串', 400)
    if (typeof value === 'string' && !/^-?\d+$/.test(value)) throw new APIError('整数格式无效', 400)
    const integer = BigInt(value)
    if (integer < -9223372036854775808n || integer > 9223372036854775807n)
      throw new APIError('整数超出允许范围', 400)
    return integer
  }
  const [key, ...rest] = path
  if (key === '*' && Array.isArray(value)) return value.map((item) => integerField(item, rest))
  if (key === '*' && value !== null && typeof value === 'object')
    return Object.fromEntries(
      Object.entries(value).map(([field, child]) => [field, integerField(child, rest)]),
    )
  if (value !== null && typeof value === 'object' && key in value) {
    return Object.fromEntries(
      Object.entries(value).map(([field, child]) => [
        field,
        field === key ? integerField(child, rest) : child,
      ]),
    )
  }
  return value
}

async function refreshSession(baseUrl: string): Promise<boolean> {
  refreshing ??= api
    .POST('/api/user/refresh', { baseUrl, body: {} })
    .then((result) => result.response.ok)
    .catch(() => false)
    .finally(() => {
      refreshing = undefined
    })
  return refreshing
}

async function transport(request: Request): Promise<Response> {
  request.headers.set('X-CodeGo-API-Version', '3')
  const retry = request.clone()
  let response = await globalThis.fetch(request)
  const url = new URL(request.url)
  if (
    response.status === 401 &&
    !/\/(login|register|logout|refresh)$/.test(url.pathname) &&
    url.pathname !== '/api/user/login/2fa' &&
    (await refreshSession(url.origin))
  ) {
    response = await globalThis.fetch(retry)
  }
  let payload: unknown
  const raw = await response.text()
  try {
    payload = decodeJSON(raw, url.pathname.startsWith('/api/marketplace/'))
  } catch {
    throw new APIError('服务器返回了无效响应', response.status)
  }
  if (!payload || typeof payload !== 'object' || !('success' in payload))
    throw new APIError('服务器返回了无效响应', response.status)
  if (!response.ok || payload.success !== true) {
    const message =
      'message' in payload && typeof payload.message === 'string'
        ? payload.message
        : `HTTP ${response.status}`
    throw new APIError(message, response.status)
  }
  // Decode once at the network boundary; operation types stay schema-derived.
  const decoded = new Response(raw, {
    status: response.status,
    statusText: response.statusText,
    headers: response.headers,
  })
  // openapi-fetch falls back to native JSON.parse when content-length is absent.
  decoded.headers.set('Content-Length', String(new TextEncoder().encode(raw).byteLength))
  Object.defineProperty(decoded, 'json', { value: async () => payload })
  return decoded
}

type Method = 'get' | 'post' | 'put' | 'patch' | 'delete'
type Options<P extends PathsWithMethod<paths, M>, M extends Method> = MaybeOptionalInit<paths[P], M>
type Arguments<P extends PathsWithMethod<paths, M>, M extends Method> =
  RequiredKeysOf<Options<P, M>> extends never
    ? [options?: Options<P, M> & { [key: string]: unknown }]
    : [options: Options<P, M> & { [key: string]: unknown }]
type TypedAPI = {
  GET: <P extends PathsWithMethod<paths, 'get'>>(
    path: P,
    ...args: Arguments<P, 'get'>
  ) => Promise<FetchResponse<paths[P]['get'], Options<P, 'get'>, 'application/json'>>
  POST: <P extends PathsWithMethod<paths, 'post'>>(
    path: P,
    ...args: Arguments<P, 'post'>
  ) => Promise<FetchResponse<paths[P]['post'], Options<P, 'post'>, 'application/json'>>
  PUT: <P extends PathsWithMethod<paths, 'put'>>(
    path: P,
    ...args: Arguments<P, 'put'>
  ) => Promise<FetchResponse<paths[P]['put'], Options<P, 'put'>, 'application/json'>>
  PATCH: <P extends PathsWithMethod<paths, 'patch'>>(
    path: P,
    ...args: Arguments<P, 'patch'>
  ) => Promise<FetchResponse<paths[P]['patch'], Options<P, 'patch'>, 'application/json'>>
  DELETE: <P extends PathsWithMethod<paths, 'delete'>>(
    path: P,
    ...args: Arguments<P, 'delete'>
  ) => Promise<FetchResponse<paths[P]['delete'], Options<P, 'delete'>, 'application/json'>>
}

const client = createClient<paths, 'application/json'>({
  credentials: 'same-origin',
  fetch: transport,
  bodySerializer: (body) =>
    stringify(body, (_key, value) => {
      if (typeof value === 'number' && Number.isInteger(value) && !Number.isSafeInteger(value))
        throw new APIError('整数超出安全范围，请使用精确字符串', 400)
      return value
    }) ?? '',
})

// Fixed operation options retain excess-property checks on payloads. The
// implementation is the generated openapi-fetch client, with no DTO casts.
export const api: TypedAPI = client

client.use({
  async onRequest({ request, schemaPath }) {
    const fields = integerRequestFields[`${request.method}:${schemaPath}`]
    if (!fields || !request.body) return
    let body = decodeJSON(await request.clone().text())
    for (const path of fields) body = integerField(body, path)
    return new Request(request, { body: stringify(body) })
  },
})

export function unwrap<T>(result: { data?: { data?: T }; response: Response }): T {
  if (result.data?.data === undefined)
    throw new APIError('服务器返回了无效响应', result.response.status)
  return result.data.data
}
