/*
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
*/
// Local, synthetic, read-only data pressure. Never forwards business mutations.
// node scripts/page-stress-fixture.mjs; edit output/.../config.json between runs.
import fs from 'node:fs'
import http from 'node:http'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const frontend = new URL(process.env.STRESS_FRONTEND || 'http://127.0.0.1:3011')
const baseline = new URL(process.env.STRESS_BASELINE || 'http://127.0.0.1:3087')
for (const url of [frontend, baseline]) {
  if (!['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)) {
    throw new Error('Stress fixture upstreams must be local.')
  }
}
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const repo = path.resolve(root, '../..')
const output = process.env.STRESS_OUTPUT
  ? path.resolve(process.env.STRESS_OUTPUT)
  : path.join(repo, 'output/playwright/page-stress')
fs.mkdirSync(output, { recursive: true })
const configPath =
  process.env.STRESS_CONFIG_PATH || path.join(output, 'config.json')
if (!fs.existsSync(configPath)) {
  fs.writeFileSync(
    configPath,
    JSON.stringify(
      { scale: 1000, mode: 'normal', delayMs: 0, target: '*' },
      null,
      2
    )
  )
}
const overrides = JSON.parse(
  fs.readFileSync(
    path.join(repo, 'output/playwright/solaris-optics/fixture-overrides.json'),
    'utf8'
  )
)
const ok = (data) => ({
  success: true,
  message: 'Synthetic stress fixture',
  data,
})
const clone = (value) => JSON.parse(JSON.stringify(value))
const template = (endpoint, key = 'items') => {
  const data = overrides[endpoint]?.data
  return clone(Array.isArray(data) ? data[0] : data?.[key]?.[0] || {})
}
const now = Math.floor(Date.now() / 1000)
const modelName = (index) =>
  index === 0
    ? 'gpt-4o'
    : `stress-${String(index).padStart(6, '0')}-${index % 2 ? 'reasoning' : 'text'}`
const title = (index) =>
  index % 29 === 0
    ? `STRESS-${index} · 跨区域多模型路由服务与超长上下文模型名称边界测试 ${'LongUnbrokenIdentifier'.repeat(6)}`
    : `STRESS ${String(index).padStart(6, '0')}`
const model = (index) => ({
  id: index + 1,
  model_name: modelName(index),
  vendor_id: (index % 4) + 1,
  vendor_name: ['OpenAI', 'Anthropic', 'Google', 'DeepSeek'][index % 4],
  quota_type: 0,
  model_ratio: ((index % 100) + 1) / 20,
  completion_ratio: 4,
  cache_ratio: 0.1,
  enable_groups: ['default'],
  supported_endpoint_types: ['openai'],
  description: title(index),
  tags: index % 2 ? 'text,reasoning' : 'text,vision',
  context_length: 128000,
  max_output_tokens: 8192,
})
const groupTemplate = template('/api/marketplace/groups')
const group = (index) => ({
  ...groupTemplate,
  id: `stress-group-${index}`,
  channel_id: String(91001 + index),
  public_slug: `stress-group-${index}`,
  system_display_name: title(index),
  source_type: index % 25 === 0 ? 'official' : 'marketplace_user',
  rank: index + 1,
  score: 99 - (index % 30),
  models: ['gpt-4o', modelName(index)],
  recent_request_series: [],
  request_count: 1000000 + index,
})
const factories = new Map([
  [
    '/api/token/',
    (index) => ({
      ...template('/api/token/'),
      id: 100000 + index,
      name: title(index),
      key: 'SYNTHETIC-NOT-A-KEY',
    }),
  ],
  [
    '/api/channel',
    (index) => ({
      ...template('/api/channel'),
      id: 100000 + index,
      name: title(index),
      key: 'SYNTHETIC-NOT-A-CREDENTIAL',
    }),
  ],
  [
    '/api/user/',
    (index) => ({
      id: 100000 + index,
      username: `stress-user-${index}`,
      display_name: title(index),
      email: `stress-${index}@example.invalid`,
      role: 1,
      status: 1,
      group: 'default',
      quota: 999999999999,
      used_quota: 999999999,
      request_count: 1000000,
    }),
  ],
  [
    '/api/models/',
    (index) => ({
      ...model(index),
      name: modelName(index),
      status: 1,
      vendor_id: (index % 4) + 1,
    }),
  ],
  [
    '/api/vendors/',
    (index) => ({
      id: index + 1,
      name: `Stress vendor ${index}`,
      description: title(index),
      status: 1,
    }),
  ],
  [
    '/api/redemption/',
    (index) => ({
      id: index + 1,
      name: title(index),
      key: 'SYNTHETIC-NOT-A-CODE',
      status: 1,
      quota: 500000000,
      created_time: now,
      redeemed_time: 0,
      expired_time: 0,
      used_user_id: 0,
    }),
  ],
  [
    '/api/log/self',
    (index) => ({
      ...template('/api/log/self'),
      id: index + 1,
      model_name: modelName(index % 100),
      content: title(index),
      created_at: now - (index % 86400),
      quota: 500000 + index,
    }),
  ],
  [
    '/api/log/',
    (index) => ({
      ...template('/api/log/self'),
      id: index + 1,
      model_name: modelName(index % 100),
      content: title(index),
      created_at: now - (index % 86400),
      quota: 500000 + index,
    }),
  ],
  [
    '/api/task/self',
    (index) => ({
      id: index + 1,
      task_id: `stress-task-${index}`,
      user_id: 900001,
      model_name: 'gpt-4o',
      platform: 'synthetic',
      action: 'GENERATE',
      status: 'SUCCESS',
      submit_time: now - index,
      start_time: now - index,
      finish_time: now - index,
      progress: '100%',
      quota: 500000,
      fail_reason: '',
      properties: {},
    }),
  ],
  [
    '/api/user/topup/self',
    (index) => ({
      ...template('/api/user/topup/self'),
      id: index + 1,
      trade_no: `STRESS-ORDER-${index}`,
      create_time: now - index,
    }),
  ],
  [
    '/api/marketplace/shops',
    (index) => ({
      id: index + 1,
      name: title(index),
      custom_name: title(index),
      moderation_status: 'approved',
      channel_count: 100,
      min_multiplier: 1,
      models: ['gpt-4o'],
      tags: [],
      rating: 4.8,
      rating_count: 1000,
      viewer_stars: 0,
      can_rate: false,
      rating_permission: 'owner',
    }),
  ],
  [
    '/api/marketplace/channels/mine/logs',
    (index) => ({
      ...template('/api/marketplace/channels/mine/logs'),
      id: index + 1,
      created_at: now - index,
      request_id: `STRESS-REQUEST-${index}`,
    }),
  ],
  [
    '/api/marketplace/channels/mine/user-usage',
    (index) => ({
      ...template('/api/marketplace/channels/mine/user-usage'),
      user_id: String(index + 1),
      external_user_id: `stress-user-${index}`,
      channel_id: '91001',
      channel_name: title(index),
    }),
  ],
  [
    '/api/marketplace/admin/security-audit/events',
    (index) => ({
      id: String(index + 1),
      event_type: 'synthetic_stress',
      severity: 'low',
      created_at: new Date((now - index) * 1000).toISOString(),
      summary: title(index),
      action: 'view',
      actor_user_id: 900001,
      details: {},
      status: 'unreviewed',
    }),
  ],
  [
    '/api/invoices/requests',
    (index) => ({
      id: index + 1,
      user_id: 900001,
      source_type: 'topup',
      trade_no: `STRESS-INVOICE-${index}`,
      order_amount: 999999999.99,
      currency: 'CNY',
      order_title: title(index),
      order_count: 1,
      invoice_type: 'personal',
      title: title(index),
      tax_number: '',
      email: 'stress@example.invalid',
      remark: '',
      status: 'pending',
      invoice_number: '',
      admin_note: '',
      handled_by: 0,
      issued_at: 0,
      created_at: now - index,
      updated_at: now - index,
    }),
  ],
  [
    '/api/deployments/',
    (index) => ({
      id: index + 1,
      deployment_name: title(index),
      name: title(index),
      status: 'running',
      provider: 'synthetic',
      compute_minutes_remaining: 999999999,
      compute_minutes_served: 999999,
      completed_percent: 50,
      hardware_info: 'Synthetic GPU',
      hardware_name: 'Synthetic GPU',
      hardware_quantity: 1,
      created_at: now - index,
      updated_at: now - index,
    }),
  ],
  [
    '/api/marketplace/admin/channels',
    (index) => ({
      ...template('/api/marketplace/channels/mine'),
      id: String(91001 + index),
      system_display_name: title(index),
      custom_name: title(index),
      declared_models: ['gpt-4o'],
    }),
  ],
])
const aliases = new Map([
  ['/api/token/search', '/api/token/'],
  ['/api/channel/search', '/api/channel'],
  ['/api/user/search', '/api/user/'],
  ['/api/models/search', '/api/models/'],
  ['/api/vendors/search', '/api/vendors/'],
  ['/api/redemption/search', '/api/redemption/'],
  ['/api/log', '/api/log/'],
  ['/api/task', '/api/task/self'],
  ['/api/user/topup', '/api/user/topup/self'],
  [
    '/api/marketplace/security-audit/events',
    '/api/marketplace/admin/security-audit/events',
  ],
  ['/api/invoices/admin/requests', '/api/invoices/requests'],
  ['/api/deployments/search', '/api/deployments/'],
])

function pageData(url, total, factory, extra = {}) {
  const page = Math.max(
    1,
    Math.floor(
      Number(url.searchParams.get('p') || url.searchParams.get('page') || 1)
    )
  )
  const cap =
    url.pathname === '/api/marketplace/channels/mine/user-usage' ? 200 : 100
  const size = Math.max(
    1,
    Math.min(
      cap,
      Math.floor(
        Number(
          url.searchParams.get('page_size') ||
            url.searchParams.get('size') ||
            20
        )
      )
    )
  )
  const start = (page - 1) * size
  const count = Math.max(0, Math.min(size, total - start))
  return {
    ...extra,
    items: Array.from({ length: count }, (_, offset) =>
      factory(start + offset)
    ),
    total,
    page,
    page_size: size,
  }
}

function stressResponse(url, scale) {
  const endpoint = aliases.get(url.pathname) || url.pathname
  if (factories.has(endpoint)) {
    const base = overrides[endpoint]?.data
    const extra =
      base && !Array.isArray(base) ? { ...base, items: undefined } : {}
    if (endpoint === '/api/channel') extra.type_counts = { 1: scale }
    if (
      endpoint.endsWith('/logs') &&
      url.searchParams.get('summary_only') === 'true'
    ) {
      return ok({
        ...extra,
        ...pageData(url, scale, factories.get(endpoint)),
        items: [],
      })
    }
    return ok(pageData(url, scale, factories.get(endpoint), extra))
  }
  if (endpoint === '/api/pricing') {
    const models = Array.from({ length: scale }, (_, index) => model(index))
    return {
      ...ok(models),
      vendors: [1, 2, 3, 4].map((id, index) => ({
        id,
        name: ['OpenAI', 'Anthropic', 'Google', 'DeepSeek'][index],
      })),
      group_ratio: { default: 1 },
      usable_group: { default: { desc: 'Synthetic', ratio: 1 } },
      supported_endpoint: {
        openai: { path: '/v1/chat/completions', method: 'POST' },
      },
      auto_groups: ['default'],
      available_models: models.map((item) => item.model_name),
    }
  }
  if (
    [
      '/api/marketplace/models',
      '/api/user/models',
      '/api/channel/models',
      '/api/channel/models_enabled',
    ].includes(endpoint)
  ) {
    return ok(Array.from({ length: scale }, (_, index) => modelName(index)))
  }
  if (endpoint === '/api/marketplace/groups') {
    return ok(
      pageData(url, scale, group, {
        official_items: scale ? [group(0)] : [],
        ranked_count: scale,
        window_hours: 24,
        highlights: {},
      })
    )
  }
  if (endpoint === '/api/marketplace/group-status')
    return ok(Array.from({ length: scale }, (_, index) => group(index)))
  if (
    endpoint === '/api/group-status' ||
    endpoint === '/api/user/self/group-status'
  ) {
    return ok(
      Array.from({ length: scale }, (_, index) => ({
        group_id: `stress-group-${index}`,
        group: `stress-group-${index}`,
        display_name: title(index),
        source_type: index % 25 === 0 ? 'official' : 'marketplace_user',
        status: 'healthy',
        request_count: 100000 + index,
        success_rate: 99.9,
        cache_hit_rate: 25,
        models: [
          {
            model: modelName(index),
            status: 'healthy',
            success_rate: 99.9,
            sample_window: 24,
            request_count: 100000 + index,
            series: [],
          },
        ],
      }))
    )
  }
  if (endpoint === '/api/marketplace/channels/mine') {
    const original = template(endpoint)
    if (url.searchParams.get('view') === 'index')
      return ok(
        Array.from({ length: scale }, (_, index) => ({
          id: String(91001 + index),
          system_display_name: title(index),
          custom_name: title(index),
          submitted_source_label: 'Synthetic',
          search_text:
            `${title(index)} ${91001 + index} synthetic gpt-4o`.toLowerCase(),
          multiplier: 1,
          lifecycle_status: 'active',
          verification_status: 'passed',
          connectivity_test_status: 'passed',
          needs_attention: false,
          checking: false,
          total_income: original.total_income || 0,
          deleted_at:
            index % 1000 === 0 ? new Date(now * 1000).toISOString() : undefined,
        }))
      )
    const requestedIDs = url.searchParams.getAll('ids')
    if (requestedIDs.length > 50)
      return { success: false, message: '每次最多查询 50 个渠道' }
    if (requestedIDs.length)
      return ok(
        requestedIDs
          .map((id) => Number(id) - 91001)
          .filter(
            (index) => Number.isInteger(index) && index >= 0 && index < scale
          )
          .map((index) => ({
            ...original,
            id: String(91001 + index),
            system_display_name: title(index),
            custom_name: title(index),
            declared_models: ['gpt-4o'],
            model_verification_results: [],
            deleted_at:
              index % 1000 === 0 ? new Date(now * 1000).toISOString() : null,
          }))
      )
    return ok(
      Array.from({ length: scale }, (_, index) => ({
        ...original,
        id: String(91001 + index),
        system_display_name: title(index),
        custom_name: title(index),
        declared_models: ['gpt-4o'],
        model_verification_results: [],
      }))
    )
  }
  if (['/api/data', '/api/data/self', '/api/data/users'].includes(endpoint)) {
    const start =
      Number(url.searchParams.get('start_timestamp')) || now - 86400 * 30
    const end = Number(url.searchParams.get('end_timestamp')) || now
    return ok(
      Array.from({ length: scale }, (_, index) => ({
        id: index + 1,
        created_at: Math.floor(
          start + ((end - start) * index) / Math.max(1, scale - 1)
        ),
        quota: 75000 + index,
        count: (index % 100) + 1,
        token_used: 12000 + index,
        model_name: modelName(index % 100),
        username: `stress-user-${index % 100}`,
      }))
    )
  }
  if (endpoint === '/api/perf-metrics/summary') {
    const original = overrides[endpoint].data
    return ok({
      ...original,
      models: Array.from({ length: scale }, (_, index) => ({
        ...original.models[0],
        model_name: modelName(index),
        request_count: 100000 + index,
      })),
    })
  }
  if (endpoint === '/api/blind-box/self') {
    const data = clone(overrides[endpoint].data)
    data.overview.available_boxes = scale
    data.overview.pending_boxes = scale ? 999999999 : 0
    data.overview.recent_records = scale
      ? Array.from({ length: Math.min(20, scale) }, (_, index) => ({
          ...data.overview.recent_records[0],
          id: index + 1,
          reward_title: title(index),
          create_time: now - index,
        }))
      : []
    data.inventory.inventory_count = scale
    data.inventory.balance_usd = scale ? 999999999999 : 0
    data.statistics.total_opened = scale
    if (!scale) {
      data.props = []
      data.statistics.rewards = []
    }
    return ok(data)
  }
  if (endpoint === '/api/blind-box/history') {
    const original = template(endpoint, 'records')
    const data = pageData(url, scale, (index) => ({
      ...original,
      id: index + 1,
      reward_title: title(index),
      create_time: now - index,
    }))
    return ok({
      ...data,
      records: data.items,
      items: undefined,
      retention_days: 30,
      cutoff_time: now - 86400 * 30,
    })
  }
  if (endpoint === '/api/wallet/transfers') {
    const base = overrides[endpoint].data
    return ok({
      ...base,
      history: pageData(url, scale, (index) => ({
        ...base.history.items[0],
        id: index + 1,
        request_id: `STRESS-TRANSFER-${index}`,
        amount_quota: 999999999999,
        created_at: now - index,
      })),
    })
  }
  if (endpoint === '/api/user/aff/overview')
    return ok({
      affiliate_code: 'synthetic',
      invited_count: scale,
      rebate_rate_bps: 300,
      rebate_effective_at: now,
      first_week_consumed_quota: 999999999999,
      rebate_total_quota: 9999999999,
      reset_opportunity: {
        available_count: 0,
        earned_total: 0,
        used_total: 0,
        used_this_month: false,
      },
      invitees: Array.from({ length: scale }, (_, index) => ({
        invitee_id: index + 1,
        invitee_external_id: `stress-invite-${index}`,
        invitee_username: title(index),
        invitee_display_name: title(index),
        created_at: now - index,
        first_week_consumed_quota: 999999999,
        rebate_quota: 30000000,
        rebate_ends_at: now + 86400 * 7,
      })),
    })
  if (endpoint === '/api/invoices/eligible-orders') {
    const order = (index) => ({
      source_type: index % 2 ? 'subscription' : 'topup',
      trade_no: `STRESS-ELIGIBLE-${String(index).padStart(6, '0')}`,
      order_title: title(index),
      order_amount: 999999999.99,
      currency: 'CNY',
      paid_at: now - index,
      requested: false,
    })
    if (
      url.searchParams.has('p') ||
      url.searchParams.has('page_size') ||
      url.searchParams.has('keyword')
    ) {
      const keyword = (url.searchParams.get('keyword') || '')
        .trim()
        .toLowerCase()
      const indexes = keyword
        ? Array.from({ length: scale }, (_, index) => index).filter((index) => {
            const item = order(index)
            return `${item.trade_no} ${item.order_title} ${item.order_amount} ${item.currency}`
              .toLowerCase()
              .includes(keyword)
          })
        : null
      return ok(
        pageData(url, indexes ? indexes.length : scale, (index) =>
          order(indexes ? indexes[index] : index)
        )
      )
    }
    return ok(Array.from({ length: scale }, (_, index) => order(index)))
  }
  if (endpoint === '/api/user/image-workspace/items') {
    const image =
      'data:image/svg+xml,' +
      encodeURIComponent(
        '<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64"><rect width="64" height="64" fill="#ff6b00"/></svg>'
      )
    const page = pageData(url, scale, (index) => ({
      id: index + 1,
      session_id: 'synthetic-stress',
      batch_id: `stress-batch-${index}`,
      source_item_id: 0,
      model: 'gpt-image-1',
      prompt: title(index),
      revised_prompt: '',
      image_index: 0,
      status: 'ready',
      image_url: image,
      download_url: '',
      original_url: '',
      expires_at: now + 86400,
      created_at: now - index,
      error_message: '',
    }))
    return { ...ok(page.items), total: page.total }
  }
  if (endpoint === '/api/deployments/settings')
    return ok({
      enabled: true,
      api_url: 'https://example.invalid',
      api_key: '',
    })
  if (
    endpoint === '/api/privacy-policy' ||
    endpoint === '/api/user-agreement'
  ) {
    return ok(
      '# Synthetic long-document boundary\n\n' +
        Array.from(
          { length: 120 },
          (_, index) =>
            `## Section ${index + 1}\n\n${'本地长文档边界验证。 '.repeat(40)}\n\n| Field | Value |\n| --- | --- |\n| ${index} | ${'LongIdentifier'.repeat(8)} |\n\n`
        ).join('')
    )
  }
  if (endpoint === '/api/performance/stats')
    return ok({
      config: { is_running_in_container: false },
      memory_stats: {
        alloc: 999999999999,
        total_alloc: 9999999999999,
        sys: 9999999999999,
        num_gc: 999999999,
        num_goroutine: 999999999,
      },
    })
  if (endpoint === '/api/performance/logs')
    return ok({
      enabled: false,
      log_dir: 'C:/synthetic/' + 'long-path-segment/'.repeat(30),
      file_count: 999999999,
      total_size: 999999999999,
    })
  return null
}

function proxy(req, res, upstream) {
  const request = http.request(
    new URL(req.url, upstream),
    { method: req.method, headers: { ...req.headers, host: upstream.host } },
    (response) => {
      res.writeHead(response.statusCode || 502, response.headers)
      response.pipe(res)
    }
  )
  request.on('error', (error) => {
    res.writeHead(502, { 'Content-Type': 'application/json' })
    res.end(
      JSON.stringify({
        success: false,
        message: `Local upstream unavailable: ${error.message}`,
      })
    )
  })
  req.pipe(request)
}

const server = http.createServer(async (req, res) => {
  const url = new URL(req.url, 'http://127.0.0.1')
  const config = JSON.parse(fs.readFileSync(configPath, 'utf8'))
  if (url.pathname === '/__stress/health') {
    res.setHeader('Content-Type', 'application/json')
    res.end(
      JSON.stringify({
        synthetic: true,
        readOnly: true,
        config,
        configPath,
        frontend: frontend.href,
        fixtureMemory: process.memoryUsage(),
      })
    )
    return
  }
  // Rsbuild development compilation endpoint; carries no application mutation.
  if (url.pathname === '/_rspack/lazy/trigger' && req.method === 'POST')
    return proxy(req, res, frontend)
  // Login and quotes are the only accepted POST requests. They remain synthetic.
  if (
    url.pathname === '/api/deployments/settings/test-connection' &&
    req.method === 'POST'
  ) {
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify(ok({ connected: true, synthetic: true })))
    return
  }
  const readOnlyPost = [
    '/api/user/login',
    '/api/user/amount',
    '/api/blind-box/amount',
    '/api/user/stripe/amount',
  ].includes(url.pathname)
  if (req.method !== 'GET' && !(req.method === 'POST' && readOnlyPost)) {
    res.writeHead(405, { 'Content-Type': 'application/json' })
    res.end(
      JSON.stringify({
        success: false,
        message: 'Stress fixture blocks business mutations',
      })
    )
    return
  }
  if (
    !url.pathname.startsWith('/api/') &&
    !url.pathname.startsWith('/__fixture/')
  )
    return proxy(req, res, frontend)
  const applies =
    config.target === '*' ||
    url.pathname === config.target ||
    (Array.isArray(config.target) && config.target.includes(url.pathname))
  if (
    !applies ||
    ['/api/status', '/api/user/self', '/api/setup'].includes(url.pathname)
  )
    return proxy(req, res, baseline)
  if (config.delayMs > 0)
    await new Promise((resolve) =>
      setTimeout(resolve, Math.min(15000, config.delayMs))
    )
  if (config.mode === 'failure') {
    res.writeHead(503, { 'Content-Type': 'application/json' })
    res.end(
      JSON.stringify({
        success: false,
        message: 'Synthetic stress service unavailable',
      })
    )
    return
  }
  const scale =
    config.mode === 'empty'
      ? 0
      : Math.max(0, Math.min(50000, Math.floor(config.scale || 1000)))
  const start = performance.now()
  const body = req.method === 'GET' ? stressResponse(url, scale) : null
  if (!body) return proxy(req, res, baseline)
  const json = JSON.stringify(body)
  res.writeHead(200, {
    'Content-Type': 'application/json; charset=utf-8',
    'Cache-Control': 'no-store',
    'X-Stress-Scale': String(scale),
    'Content-Length': String(Buffer.byteLength(json)),
    'X-Stress-Rows': String(
      body.data?.items?.length ??
        body.data?.records?.length ??
        (Array.isArray(body.data) ? body.data.length : '')
    ),
    'X-Stress-Total': String(body.data?.total ?? ''),
    'X-Stress-Success': String(body.success !== false),
  })
  res.end(json)
  fs.appendFileSync(
    path.join(output, 'requests.ndjson'),
    JSON.stringify({
      at: new Date().toISOString(),
      path: url.pathname,
      query: url.search,
      scale,
      bytes: Buffer.byteLength(json),
      fixtureMs: Number((performance.now() - start).toFixed(2)),
      rows:
        body.data?.items?.length ??
        body.data?.records?.length ??
        (Array.isArray(body.data) ? body.data.length : null),
      total: body.data?.total ?? null,
    }) + '\n'
  )
})
const port = Number(process.env.STRESS_PORT || 3097)
server.listen(port, '127.0.0.1', () =>
  console.log(
    `Read-only synthetic stress UI: http://127.0.0.1:${port}; config: ${configPath}`
  )
)
