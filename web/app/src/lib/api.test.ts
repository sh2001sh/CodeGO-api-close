import { afterEach, describe, expect, it, vi } from 'vitest'
import { notificationPresentation } from '../features/notifications/presentation'
import { factorText, ppmFactorText } from './factor'

afterEach(() => vi.unstubAllGlobals())
const response = (data: string, status = 200) =>
  new Response(data, { status, headers: { 'Content-Type': 'application/json' } })
const baseUrl = 'http://localhost'

describe('control API transport', () => {
  it('serializes int64 adjustments as exact JSON integers', async () => {
    const mock = vi.fn((_request: Request) =>
      Promise.resolve(response('{"success":true,"data":{}}')),
    )
    vi.stubGlobal('fetch', mock)
    vi.resetModules()
    const { api } = await import('./api')
    await api.POST('/api/billing/adjustments', {
      baseUrl,
      body: {
        account_id: 1,
        amount_micro: 9223372036854775807n,
        operation_id: 'adjustment-1',
        reason: 'test',
      },
    })
    const request = mock.mock.calls[0][0]
    expect(await request.text()).toBe(
      '{"account_id":1,"amount_micro":9223372036854775807,"operation_id":"adjustment-1","reason":"test"}',
    )
  })
  it('preserves monetary integers that native JSON.parse would round', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          response(
            '{"success":true,"data":{"account_id":1,"balance_micro":9223372036854775807,"version":1,"balance_micro_credits":"9223372036854775807"}}',
          ),
        ),
      ),
    )
    vi.resetModules()
    const { api, unwrap } = await import('./api')
    const result = unwrap(await api.GET('/api/wallet', { baseUrl }))
    expect(result.balance_micro).toBe('9223372036854775807')
  })
  it('keeps fractional historical PPM lossless without changing unrelated decimals', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          response(
            '{"success":true,"data":[{"previous_multiplier_ppm":131145.14191981,"multiplier_ppm":1e-57,"proposed_ppm":"1e-57","success_rate":99.125,"count":3}]}',
          ),
        ),
      ),
    )
    vi.resetModules()
    const { api, unwrap } = await import('./api')
    const rows = unwrap(await api.GET('/api/marketplace/multiplier-notices', { baseUrl }))
    expect(rows[0]).toEqual({
      previous_multiplier_ppm: '131145.14191981',
      multiplier_ppm: '1e-57',
      proposed_ppm: '1e-57',
      success_rate: 99.125,
      count: 3,
    })
  })
  it('keeps historical bargain multipliers exact and official catalog decimals numeric', async () => {
    const mock = vi
      .fn()
      .mockResolvedValueOnce(
        response(
          '{"success":true,"data":[{"proposed_multiplier":1e-63,"multiplier":0.13114514191981}]}',
        ),
      )
      .mockResolvedValueOnce(response('{"success":true,"data":[{"multiplier":0.8}]}'))
    vi.stubGlobal('fetch', mock)
    vi.resetModules()
    const { api, unwrap } = await import('./api')
    const bargains = unwrap(
      await api.GET('/api/marketplace/channels/mine/bargain-requests', { baseUrl }),
    )
    expect(bargains[0]).toEqual({ proposed_multiplier: '1e-63', multiplier: '0.13114514191981' })
    const groups = unwrap(await api.GET('/api/catalog/groups', { baseUrl }))
    expect(groups[0].multiplier).toBe(0.8)
  })
  it('preserves retained activity discounts across transport and display', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          response(
            '{"success":true,"data":[{"multiplier":1e-8,"multiplier_ppm":0.01},{"multiplier":1e-20,"multiplier_ppm":1e-14},{"multiplier":1.234567891234567891e-20,"multiplier_ppm":1.234567891234567891e-14},{"multiplier":0,"multiplier_ppm":0}]}',
          ),
        ),
      ),
    )
    vi.resetModules()
    const { api, unwrap } = await import('./api')
    const rules = unwrap(
      await api.GET('/api/marketplace/channels/{id}/time-range-multipliers', {
        baseUrl,
        params: { path: { id: 'channel-1' } },
      }),
    )
    expect(rules[0].multiplier).toBe('1e-8')
    expect(rules[0].multiplier_ppm).toBe('0.01')
    expect(rules[1].multiplier).toBe('1e-20')
    expect(rules[1].multiplier_ppm).toBe('1e-14')
    expect(rules[2].multiplier).toBe('1.234567891234567891e-20')
    expect(rules[2].multiplier_ppm).toBe('1.234567891234567891e-14')
    const expectedFactors = [
      '0.00000001',
      `0.${'0'.repeat(19)}1`,
      `0.${'0'.repeat(19)}1234567891234567891`,
      '0',
    ]
    for (const [index, expected] of expectedFactors.entries()) {
      expect(factorText(rules[index].multiplier)).toBe(expected)
      expect(ppmFactorText(rules[index].multiplier_ppm)).toBe(expected)
    }
  })
  it('preserves tiny bargain factors embedded in notification data', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          response(
            '{"success":true,"data":{"items":[{"kind":"bargain_requested","data":{"channel_id":"1","proposed_ppm":1e-57}}]}}',
          ),
        ),
      ),
    )
    vi.resetModules()
    const { api, unwrap } = await import('./api')
    const notifications = unwrap(await api.GET('/api/notifications', { baseUrl }))
    expect(notifications.items[0].data?.proposed_ppm).toBe('1e-57')
    expect(notificationPresentation(notifications.items[0], '—').parameters?.proposed).toBe(
      `0.${'0'.repeat(62)}1`,
    )
  })
  it('serializes schema int64 strings as JSON integers and rejects invalid boundaries', async () => {
    const mock = vi.fn((_request: Request) =>
      Promise.resolve(response('{"success":true,"data":{}}')),
    )
    vi.stubGlobal('fetch', mock)
    vi.resetModules()
    const { api, APIError } = await import('./api')
    const body = {
      account_id: '9223372036854775807',
      amount_micro: '-9223372036854775808',
      operation_id: 'adjustment-2',
      reason: '12345',
    }
    await api.POST('/api/billing/adjustments', { baseUrl, body })
    expect(await mock.mock.calls[0][0].text()).toBe(
      '{"account_id":9223372036854775807,"amount_micro":-9223372036854775808,"operation_id":"adjustment-2","reason":"12345"}',
    )
    await expect(
      api.POST('/api/billing/adjustments', {
        baseUrl,
        body: { ...body, amount_micro: '9223372036854775808' },
      }),
    ).rejects.toEqual(new APIError('整数超出允许范围', 400))
    await expect(
      api.POST('/api/billing/adjustments', {
        baseUrl,
        body: { ...body, amount_micro: 9007199254740992 },
      }),
    ).rejects.toEqual(new APIError('整数超出安全范围，请使用精确字符串', 400))
    expect(mock).toHaveBeenCalledTimes(1)
  })
  it('keeps server failure messages and rejects non-JSON failures', async () => {
    const mock = vi
      .fn()
      .mockResolvedValueOnce(response('{"success":false,"message":"账本服务暂时不可用"}', 503))
      .mockResolvedValueOnce(response('upstream failed', 502))
    vi.stubGlobal('fetch', mock)
    vi.resetModules()
    const { api, APIError } = await import('./api')
    await expect(api.GET('/api/wallet', { baseUrl })).rejects.toEqual(
      new APIError('账本服务暂时不可用', 503),
    )
    await expect(api.GET('/api/wallet', { baseUrl })).rejects.toEqual(
      new APIError('服务器返回了无效响应', 502),
    )
  })
  it('serializes monetary map values exactly and rejects overflowing model caps', async () => {
    const mock = vi.fn((_request: Request) =>
      Promise.resolve(response('{"success":true,"data":{}}')),
    )
    vi.stubGlobal('fetch', mock)
    vi.resetModules()
    const { api, APIError } = await import('./api')
    const body = {
      name: '12345',
      price_minor: 100,
      currency: 'usd',
      upgrade_group: 'vip',
      model_limits: { '12345': '9007199254740993', 'gpt-4o': 9223372036854775807n },
    }
    await api.POST('/api/subscription/admin/plans', { baseUrl, body })
    expect(await mock.mock.calls[0][0].text()).toBe(
      '{"name":"12345","price_minor":100,"currency":"usd","upgrade_group":"vip","model_limits":{"12345":9007199254740993,"gpt-4o":9223372036854775807}}',
    )
    await expect(
      api.POST('/api/subscription/admin/plans', {
        baseUrl,
        body: { ...body, model_limits: { 'gpt-4o': '9223372036854775808' } },
      }),
    ).rejects.toEqual(new APIError('整数超出允许范围', 400))
    expect(mock).toHaveBeenCalledTimes(1)
  })
  it('refreshes one session for concurrent expired requests and retries each once', async () => {
    let refreshed = false
    let refreshCount = 0
    const mock = vi.fn(async (request: Request) => {
      if (new URL(request.url).pathname === '/api/user/refresh') {
        refreshCount++
        await new Promise((resolve) => setTimeout(resolve, 10))
        refreshed = true
        return response('{"success":true,"data":{}}')
      }
      return refreshed
        ? response('{"success":true,"data":{}}')
        : response('{"success":false,"message":"session_expired"}', 401)
    })
    vi.stubGlobal('fetch', mock)
    vi.resetModules()
    const { api } = await import('./api')
    await Promise.all([api.GET('/api/wallet', { baseUrl }), api.GET('/api/user/self', { baseUrl })])
    expect(refreshCount).toBe(1)
    expect(mock).toHaveBeenCalledTimes(5)
  })
  it('stops after one retry and keeps authentication failure messages', async () => {
    const mock = vi.fn(async (request: Request) =>
      new URL(request.url).pathname === '/api/user/refresh'
        ? response('{"success":true,"data":{}}')
        : response('{"success":false,"message":"访问已停用"}', 401),
    )
    vi.stubGlobal('fetch', mock)
    vi.resetModules()
    const { api, APIError } = await import('./api')
    await expect(api.GET('/api/wallet', { baseUrl })).rejects.toEqual(
      new APIError('访问已停用', 401),
    )
    expect(mock).toHaveBeenCalledTimes(3)
  })
})
