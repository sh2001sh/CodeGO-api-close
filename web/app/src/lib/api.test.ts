import { afterEach, describe, expect, it, vi } from 'vitest'

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
