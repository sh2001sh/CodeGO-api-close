/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// Real Chromium acceptance against a local frontend. Every API is intercepted;
// this script cannot validate provider settlement, ledger writes, or production.
// Usage: node scripts/redesign-acceptance.mjs [--quick] [--interactions-only]
// Optional: ACCEPTANCE_BASE_URL, ACCEPTANCE_PLAYWRIGHT_MODULE, ACCEPTANCE_LABEL.
import fs from 'node:fs/promises'
import path from 'node:path'
import os from 'node:os'
import { fileURLToPath, pathToFileURL } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const baseURL = process.env.ACCEPTANCE_BASE_URL || 'http://127.0.0.1:3001'
if (!['127.0.0.1', 'localhost', '[::1]'].includes(new URL(baseURL).hostname)) {
  throw new Error('Acceptance is restricted to a local frontend.')
}
const label = (process.env.ACCEPTANCE_LABEL || 'baseline').replace(/[^a-z0-9_-]/gi, '-')
const output = path.join(root, 'output/playwright', label)
const theme = process.env.ACCEPTANCE_THEME === 'dark' ? 'dark' : 'light'
await fs.mkdir(output, { recursive: true })

async function getPlaywright() {
  if (process.env.ACCEPTANCE_PLAYWRIGHT_MODULE) {
    return import(pathToFileURL(process.env.ACCEPTANCE_PLAYWRIGHT_MODULE).href)
  }
  try { return await import('playwright') } catch { /* CLI cache fallback */ }
  const cache = path.join(process.env.LOCALAPPDATA || path.join(os.homedir(), 'AppData/Local'), 'npm-cache/_npx')
  for (const dir of await fs.readdir(cache)) {
    const module = path.join(cache, dir, 'node_modules/playwright/index.mjs')
    try { await fs.access(module); return await import(pathToFileURL(module).href) } catch { /* next cached CLI */ }
  }
  throw new Error('Run npx --package @playwright/cli playwright-cli --help first, or set ACCEPTANCE_PLAYWRIGHT_MODULE.')
}

const sectionCategories = ['site', 'auth', 'billing', 'models', 'security', 'content', 'operations']
const sections = []
for (const category of sectionCategories) {
  const text = await fs.readFile(path.join(root, `src/features/system-settings/${category}/section-registry.tsx`), 'utf8')
  for (const match of text.matchAll(/\bid:\s*'([^']+)'/g)) sections.push(`/system-settings/${category}/${match[1]}`)
}
const routesText = await fs.readFile(path.join(root, 'src/routeTree.gen.ts'), 'utf8')
const dynamic = {
  '/pricing/$modelId': ['/pricing/gpt-4.1'],
  '/topics/$slug': ['/topics/codex-api'],
  '/oauth/$provider': ['/oauth/github'],
  '/chat/$chatId': ['/chat/0'],
  '/errors/$error': ['/errors/forbidden'],
  '/dashboard/$section': ['/dashboard/overview', '/dashboard/models', '/dashboard/users'],
  '/models/$section': ['/models/metadata', '/models/deployments'],
  '/usage-logs/$section': ['/usage-logs/common', '/usage-logs/task'],
}
const routes = new Set()
for (const match of routesText.matchAll(/fullPath:\s*'([^']+)'/g)) {
  const route = match[1].replace(/\/$/, '') || '/'
  if (route.startsWith('/system-settings/') && route.includes('$')) continue
  if (route.includes('$')) (dynamic[route] || []).forEach((item) => routes.add(item))
  else routes.add(route)
}
sections.forEach((section) => routes.add(section))
const topicSource = await fs.readFile(path.join(root, 'src/features/search-pages/data.ts'), 'utf8')
for (const match of topicSource.matchAll(/\bslug:\s*'([^']+)'/g)) routes.add(`/topics/${match[1]}`)
routes.add('/wallet?section=invoices')
routes.add('/wallet?section=transfer')
routes.add('/wallet?section=billing')
routes.add('/market?browse=shops')
routes.add('/market?shop_id=1&shop_name=Acceptance%20Shop')
const routeList = [...routes]
const designSource = await fs.readFile(path.join(root, '../../docs/redesign/14-route-and-component-coverage.md'), 'utf8')
const designObjects = [...designSource.matchAll(/^\| (P\d+) \| ([^|]+) \| `([^`]+)`/gm)].filter((match) => Number(match[1].slice(1)) <= 96).map((match) => ({ id: match[1], name: match[2].trim(), specification: match[3], type: ['P05', 'P06', 'P45'].includes(match[1]) ? 'subview' : 'page', acceptance: match[1] === 'P05' ? 'interaction: market group expansion' : match[1] === 'P06' ? '/market?shop_id=1&shop_name=Acceptance%20Shop' : match[1] === 'P45' ? '/wallet?section=invoices' : match[3].replace('$modelId', 'gpt-4.1').replace('$slug', 'codex-api').replace('$provider', 'github').replace('$chatId', '0').replace('$error', 'forbidden') }))
await fs.writeFile(path.join(output, 'route-manifest.json'), JSON.stringify({ designObjects, sections, routes: routeList }, null, 2))
if (process.argv.includes('--manifest-only')) {
  console.log(JSON.stringify({ routes: routeList.length, settingsSections: sections.length, output }))
  process.exit(0)
}

const user = { id: 970001, username: 'acceptance', display_name: 'Acceptance', email: 'acceptance@example.test', role: 100, status: 1, group: 'default', quota: 50000000, used_quota: 1000000, request_count: 1200, aff_code: 'acceptance', aff_count: 0, aff_quota: 0, aff_history_quota: 0, setting: '{}', permissions: { sidebar_settings: true } }
const status = { system_name: 'CodeGo', version: 'acceptance', register_enabled: true, password_register_enabled: true, password_login_enabled: true, quota_per_unit: 500000, quota_display_type: 'USD', usd_exchange_rate: 1, display_in_currency: true, enable_online_topup: true, user_agreement_enabled: true, privacy_policy_enabled: true, email_verification: false, turnstile_check: false, checkin_enabled: true, Chats: JSON.stringify([{ 'Acceptance Chat': `${baseURL}/acceptance-chat-frame` }]) }
const models = [
  { id: 1, model_name: 'gpt-4.1', vendor_id: 1, vendor_name: 'OpenAI', quota_type: 0, model_ratio: 1, completion_ratio: 4, enable_groups: ['default'], tags: 'text,vision', supported_endpoint_types: ['openai'], context_length: 1048576, max_output_tokens: 32768 },
  { id: 2, model_name: 'claude-sonnet-4', vendor_id: 2, vendor_name: 'Anthropic', quota_type: 0, model_ratio: 1.5, completion_ratio: 5, enable_groups: ['default'], tags: 'text', supported_endpoint_types: ['anthropic'], context_length: 200000, max_output_tokens: 64000 },
]
const group = { id: 'acceptance-group', channel_id: 'acceptance-channel', public_slug: 'acceptance-group', system_display_name: 'Acceptance Route', source_type: 'official', source_label: 'OpenAI', provider_type: 'openai', credit_pool_policy: 'shared', lifecycle_status: 'active', verification_status: 'passed', verification_stage: 'completed', verification_summary: '', verification_detector_version: '1', multiplier: 1, subscription_enabled: true, subscription_multiplier: 1, credit_pack_supported: true, multiplier_card_supported: true, multiplier_card_user_enabled: true, models: ['gpt-4.1'], model_verification_results: [], connectivity_test_status: 'passed', model_consistency_status: 'passed', auto_probe_enabled: false, auto_probe_interval_minutes: 60, auto_probe_model: 'gpt-4.1', channel_feedback: { passed: 10, failed: 0, questionable: 0, total: 10, viewer_status: '' }, can_submit_channel_feedback: true, channel_feedback_permission: 'allowed', rank: 1, score: 98, success_rate: 99.9, wilson_success_rate: 99, avg_ttft_ms: 230, attempt_ttft_p50_ms: 230, attempt_ttft_p95_ms: 400, e2e_ttft_p50_ms: 240, e2e_ttft_p95_ms: 450, latency_sample_count: 100, avg_latency_ms: 500, avg_tps: 80, cache_hit_rate: 40, avg_consumer_amount: 0.002, avg_consumer_amount_by_model: {}, latest_request_status: 'healthy', recent_request_series: [], recent_request_bucket_seconds: 3600, request_count: 1200, max_concurrency: 100, user_max_concurrency: 10, current_concurrency: 2, observing: false, updated_at: new Date().toISOString() }
const key = { id: 1, name: 'Acceptance Key', key: 'masked-test-fixture', status: 1, remain_quota: 10000000, used_quota: 1000, unlimited_quota: false, expired_time: -1, created_time: 1780000000, accessed_time: 1780000000, group: 'default', cross_group_retry: false, marketplace_multiplier_limit: 0, model_limits_enabled: false, model_limits: '', allow_ips: '' }
const shop = { id: 1, name: 'Acceptance Shop', custom_name: '', moderation_status: 'approved', description: '', channel_count: 1, min_multiplier: 1, models: ['gpt-4.1'], tags: [], rating: 4.8, rating_count: 10, viewer_stars: 0, can_rate: false, rating_permission: 'owner' }
const resetOpportunity = { available_count: 0, earned_total: 0, used_total: 0, used_this_month: false, current_month: '2026-10', last_used_month: '' }
const subscription = { billing_preference: 'subscription_first', funding_source_order: ['subscription', 'wallet'], subscription_order_ids: [], subscriptions: [], all_subscriptions: [], conversion_config: { enabled: true }, recent_conversions: [], reset_opportunity: resetOpportunity }
const topup = { enable_online_topup: true, enable_stripe_topup: false, pay_methods: [{ name: 'Acceptance Pay', type: 'alipay', min_topup: 1 }], min_topup: 1, stripe_min_topup: 1, amount_options: [10, 25, 50, 100], discount: {}, enable_redemption: true, payment_compliance_confirmed: true }
const settingsOptions = [{ key: 'SystemName', value: 'CodeGo' }, { key: 'ServerAddress', value: 'https://api.example.test' }, { key: 'Notice', value: '' }]
const paginate = (items = []) => ({ items, total: items.length, page: 1, page_size: 20, pageSize: 20 })
const ok = (data) => ({ success: true, message: '', data })
const { chromium } = await getPlaywright()
let browser
try { browser = await chromium.launch({ headless: true }) } catch { browser = await chromium.launch({ headless: true, channel: 'chrome' }) }
if (process.argv.includes('--browser-check')) {
  const page = await browser.newPage()
  await page.goto('about:blank')
  console.log(JSON.stringify({ browser: browser.version(), page: page.url(), routes: routeList.length, sections: sections.length }))
  await browser.close()
  process.exit(0)
}
async function previousEvidence(filename) {
  if (process.env.ACCEPTANCE_MERGE !== 'true') return []
  try { return JSON.parse(await fs.readFile(path.join(output, filename), 'utf8')) } catch (error) { if (error.code === 'ENOENT') return []; throw error }
}
const results = await previousEvidence('page-results.json')
const interactions = await previousEvidence('interaction-results.json')
const unknown = new Set()
const allRequests = await previousEvidence('api-requests.json')
const openContexts = new Set()
const contextRequests = new WeakMap()
let interactionRuntimeErrors = null
let writeQueue = Promise.resolve()
const aliases = {
  '/brand': '/', '/status': '/group-status', '/console/topup': '/wallet', '/console/log': '/usage-logs/common',
  '/invoices': '/wallet', '/daily-lucky-number': '/wallet', '/group-buy': '/wallet',
  '/dashboard': '/dashboard/overview', '/models': '/models/metadata', '/usage-logs': '/usage-logs/common',
  '/system-settings': '/system-settings/site/system-info',
  ...Object.fromEntries(sectionCategories.map((category) => [`/system-settings/${category}`, sections.find((section) => section.startsWith(`/system-settings/${category}/`))])),
}
const callbackRoutes = new Set(['/oauth', '/oauth/github'])
const testURLs = { '/desktop/authorize': '/desktop/authorize?session_id=acceptance-session&code=ACCEPT', '/oauth/github': '/oauth/github?code=acceptance-code&state=acceptance-state' }

async function mockContext({ role = 100, authenticated = true, setupReady = true, mode = 'populated', control = {}, viewport = { width: 1440, height: 1000 } } = {}) {
  const identity = { ...user, role }
  const options = structuredClone(settingsOptions)
  const tokenRows = mode === 'empty' ? [] : [{ ...structuredClone(key), ...control.keyDetails }]
  const poolConfig = { strategy: 'priority', max_attempts: 3, failure_cooldown_seconds: 60, max_multiplier: 0 }
  const poolAutoBuild = { enabled: false, credit_pack_only: false, schedule: 'interval', interval_minutes: 60, daily_time: '00:00', models: [], consumer_weight: 25, success_weight: 25, ttft_weight: 25, cache_weight: 25, size: 2, explore: 0 }
  const poolItem = (id, name, priority) => ({ ...group, group_id: id, system_display_name: name, availability: 100, metrics_available: true, route_score: 98, selected: true, priority })
  const poolRows = control.poolMembers ? [{ id: 'acceptance-pool', name: 'Acceptance Pool', token_group: 'pool_acceptance', selected_count: 2, items: [poolItem(group.id, group.system_display_name, 0), poolItem('second-group', 'Second Route', 1)], config: structuredClone(poolConfig), auto_build: structuredClone(poolAutoBuild) }] : []
  control.options = options
  control.tokenRows = tokenRows
  control.poolRows = poolRows
  const context = await browser.newContext({ viewport, colorScheme: theme, reducedMotion: 'reduce', locale: 'zh-CN', serviceWorkers: 'block' })
  openContexts.add(context)
  contextRequests.set(context, [])
  context.on('close', () => openContexts.delete(context))
  context.on('page', (page) => page.on('pageerror', (error) => interactionRuntimeErrors?.push(String(error))))
  context.setDefaultTimeout(7000)
  await context.addInitScript(({ identity, status, authenticated, theme }) => {
    if (!['http:', 'https:'].includes(location.protocol)) return
    localStorage.setItem('setup_status_checked', 'true')
    localStorage.setItem('i18nextLng', 'zh')
    localStorage.setItem('status', JSON.stringify(status))
    localStorage.setItem('theme', theme)
    if (authenticated) { localStorage.setItem('user', JSON.stringify(identity)); localStorage.setItem('uid', String(identity.id)) }
    else { localStorage.removeItem('user'); localStorage.removeItem('uid') }
  }, { identity, status, authenticated, theme })
  await context.route('**/*', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.origin !== new URL(baseURL).origin && !['data:', 'blob:'].includes(url.protocol)) return route.abort()
    if (url.pathname === '/acceptance-chat-frame') return route.fulfill({ contentType: 'text/html', body: '<!doctype html><html><body><main>Acceptance embedded chat fixture</main></body></html>' })
    if (!/^\/(api|v1|mj|pg)\b/.test(url.pathname)) return route.continue()
    const p = url.pathname.replace(/\/$/, '')
    const method = request.method()
    const event = { path: p, search: url.search, method, body: request.postData(), mode, fixture: 'handled' }
    allRequests.push(event)
    contextRequests.get(context).push(event)
    const fulfill = (body, responseStatus = 200) => route.fulfill({ status: responseStatus, contentType: 'application/json', body: JSON.stringify(body) })
    if (p === '/api/status') return fulfill(ok(status))
    if (p === '/api/setup') return fulfill(ok({ status: setupReady, root_init: setupReady, database_type: 'sqlite' }))
    if (p === '/api/user/self') return fulfill(authenticated ? ok(identity) : { success: false, message: 'Session expired' }, authenticated ? 200 : 401)
    if (p === '/api/oauth/github') { await new Promise((resolve) => setTimeout(resolve, 1000)); return fulfill(ok(identity)) }
    if (mode === 'error') return fulfill({ success: false, message: 'Acceptance API unavailable' }, 503)
    if (method !== 'GET') {
      const body = JSON.parse(request.postData() || '{}')
      if (control.reject?.({ path: p, method, body, search: url.search })) return fulfill({ success: false, message: 'Acceptance mutation rejected' })
      if (p === '/api/option') { const prev = options.find((o) => o.key === body.key); if (prev) prev.value = String(body.value); else options.push({ ...body, value: String(body.value) }); control.afterOptionWrite?.(body, options); return fulfill(ok(null)) }
      if (p === '/api/token' && method === 'POST') { const created = { ...key, ...body, id: 2 }; tokenRows.push(created); return fulfill(ok(created)) }
      if (p === '/api/token' && method === 'PUT') { const current = tokenRows.find((item) => item.id === body.id); if (!current) return fulfill({ success: false, message: 'Fixture key missing' }); Object.assign(current, body); return fulfill(ok(current)) }
      if (/^\/api\/token\/\d+$/.test(p) && method === 'DELETE') { const index = tokenRows.findIndex((item) => item.id === Number(p.split('/').at(-1))); if (index >= 0) tokenRows.splice(index, 1); return fulfill(ok(null)) }
      if (p === '/api/marketplace/route-pools' && method === 'POST') { const created = { id: 'created-pool', name: body.name, token_group: 'pool_created', selected_count: 0, items: [], config: { ...poolConfig, ...body.config }, auto_build: { ...poolAutoBuild, ...body.auto_build } }; poolRows.push(created); return fulfill(ok(created)) }
      if (/^\/api\/marketplace\/route-pools\/[^/]+$/.test(p)) { const index = poolRows.findIndex((item) => item.id === p.split('/').at(-1)); if (index < 0) return fulfill({ success: false, message: 'Fixture pool missing' }); if (method === 'DELETE') { poolRows.splice(index, 1); return fulfill(ok(null)) }; const pool = poolRows[index]; if (body.name) pool.name = body.name; if (body.config) Object.assign(pool.config, body.config); if (body.auto_build) Object.assign(pool.auto_build, body.auto_build); if (body.group_ids) { pool.items = body.group_ids.map((id, priority) => ({ ...pool.items.find((item) => item.group_id === id), selected: true, priority })); pool.selected_count = pool.items.length }; return fulfill(ok(pool)) }
      if (p.includes('/amount')) return fulfill(ok('10.00'))
      if (p.endsWith('/pay')) return fulfill({ success: false, message: 'Acceptance payment rejected' })
      if (p === '/v1/chat/completions' || p === '/pg/chat/completions') {
        const payload = JSON.parse(request.postData() || '{}')
        if (payload.stream) return route.fulfill({ status: 200, contentType: 'text/event-stream', body: 'data: ' + JSON.stringify({ id: 'acceptance', model: 'gpt-4.1', choices: [{ index: 0, delta: { content: 'Acceptance reply' }, finish_reason: null }] }) + '\n\ndata: ' + JSON.stringify({ id: 'acceptance', choices: [{ index: 0, delta: {}, finish_reason: 'stop' }], usage: { prompt_tokens: 1, completion_tokens: 2 } }) + '\n\ndata: [DONE]\n\n' })
        return fulfill({ id: 'acceptance', model: 'gpt-4.1', object: 'chat.completion', choices: [{ index: 0, message: { role: 'assistant', content: 'Acceptance reply' }, finish_reason: 'stop' }], usage: { prompt_tokens: 1, completion_tokens: 2 } })
      }
      return fulfill(ok(null))
    }
    if (p === '/api/pricing') return fulfill({ ...ok(mode === 'empty' ? [] : models), vendors: [{ id: 1, name: 'OpenAI' }, { id: 2, name: 'Anthropic' }], group_ratio: { default: 1 }, usable_group: { default: { desc: 'Default', ratio: 1 } }, supported_endpoint: { openai: { path: '/v1/chat/completions', method: 'POST' } }, auto_groups: [] })
    if (p === '/api/option') return fulfill(ok(options))
    if (/^\/api\/token\/\d+$/.test(p)) { if (control.keyDetailDelay) await new Promise((resolve) => setTimeout(resolve, control.keyDetailDelay)); if (control.keyDetailFailure) return fulfill({ success: false, message: 'Acceptance detail rejected' }); return fulfill(ok(tokenRows.find((item) => item.id === Number(p.split('/').at(-1))))) }
    if (p === '/api/marketplace/route-pools') return fulfill(ok(poolRows.map((pool) => ({ ...pool, member_count: pool.selected_count, models: ['gpt-4.1'] }))))
    if (/^\/api\/marketplace\/route-pools\/[^/]+$/.test(p)) return fulfill(ok(poolRows.find((pool) => pool.id === p.split('/').at(-1))))
    if (p === '/api/wallet/transfers/recipients/ABC123') return fulfill(ok({ external_id: 'ABC123', display_name_masked: 'A***' }))
    if (p === '/api/group-buy/list' || p === '/api/group-buy/mine') return fulfill(ok({ data: [], total: 0 }))
    if (p === '/api/packages/public') return fulfill(ok(mode === 'empty' ? [] : [{ plan: { id: 1, title: 'Acceptance Credits', price_amount: 10, currency: 'USD', duration_unit: 'permanent', duration_value: 1, quota_reset_period: 'never', enabled: true, internal_only: false, sort_order: 1, max_purchase_per_user: 0, plan_type: 'credit_pack', total_amount: 5000000, period_amount: 0, model_limits: '', upgrade_group: '' }, action: 'subscribe' }]))
    if (p === '/api/user/self/group-status' || p === '/api/group-status') return fulfill(ok(mode === 'empty' ? [] : [{ group_id: group.id, group: 'default', display_name: group.system_display_name, source_type: 'official', status: 'healthy', request_count: 1200, success_rate: 99.9, cache_hit_rate: 40, models: [{ model: 'gpt-4.1', status: 'healthy', success_rate: 99.9, sample_window: 21600, series_window: 21600, bucket_seconds: 3600, request_count: 1200, cache_hit_rate: 40, series: [{ ts: 1792051200, success_rate: 99.9, request_count: 1200 }] }] }]))
    if (p === '/api/desktop/release/latest') return fulfill({ tag_name: 'v1.0.0-acceptance', version: '1.0.0-acceptance', published_at: '2026-10-16T00:00:00Z', html_url: `${baseURL}/acceptance-release`, assets: ['windows', 'macos', 'linux'].map((platform) => ({ name: `CodeGo-${platform}.${platform === 'windows' ? 'msi' : platform === 'macos' ? 'dmg' : 'AppImage'}`, size: 100000000, platform, arch: 'x64', browser_download_url: `${baseURL}/acceptance-download/${platform}` })) })
    if (p === '/api/billing/reconciliations') return fulfill(ok(mode === 'empty' ? [] : [{ account_id: 'acceptance-account', consistent: true, actual: { available_balance: 100, reserved_balance: 0 }, expected: { available_balance: 100, reserved_balance: 0 } }]))
    if (p === '/api/deployments/settings') return fulfill(ok({ enabled: true }))
    if (p === '/api/invoices/eligible-orders') return fulfill(ok(mode === 'empty' ? [] : [{ source_type: 'topup', trade_no: 'ACCEPTANCE-ORDER', order_title: 'Acceptance Topup', order_amount: 10, currency: 'CNY', paid_at: 1792051200, requested: false }]))
    if (p === '/api/invoices/requests' || p === '/api/invoices/admin/requests') return fulfill(ok(paginate()))
    if (p === '/api/user/image-workspace/models') return fulfill(ok(mode === 'empty' ? [] : ['gpt-image-1']))
    if (p === '/api/user/image-workspace/items') return fulfill({ ...ok([]), total: 0 })
    if (p === '/api/route-pools/groups') return fulfill(ok({ items: mode === 'empty' ? [] : [{ group: 'default', pool_id: 1, enabled: true, algorithm_active: false, auto_discover: false, channels: [] }] }))
    if (p === '/api/route-finance/policies') return fulfill(ok({ items: ['topup', 'blind_box', 'subscription'].map((source) => ({ source, revenue_multiplier: 1 })) }))
    if (p === '/api/route-finance/daily') return fulfill(ok({ recognized_revenue: 10, recognized_profit: 1, sources: [] }))
    if (p === '/api/channel/models') return fulfill(ok(mode === 'empty' ? [] : models.map((model) => ({ id: model.model_name }))))
    if (p === '/api/option/channel_affinity_cache') return fulfill(ok({ enabled: true, total: 0, unknown: 0, by_rule_name: {}, cache_capacity: 1000, cache_algo: 'lru' }))
    if (p === '/api/performance/stats') return fulfill(ok({ cache_stats: { current_disk_usage_bytes: 0, disk_cache_max_bytes: 104857600, active_disk_files: 0, disk_cache_hits: 0, current_memory_usage_bytes: 1048576, active_memory_buffers: 1, memory_cache_hits: 0 }, disk_space_info: { total: 107374182400, free: 53687091200, used: 53687091200, used_percent: 50 }, memory_stats: { alloc: 1048576, total_alloc: 2097152, sys: 4194304, num_gc: 1, num_goroutine: 4 }, disk_cache_info: { path: '/acceptance-cache', file_count: 0, total_size: 0 }, config: { is_running_in_container: false } }))
    if (p === '/api/performance/logs') return fulfill(ok({ enabled: true, log_dir: '/acceptance-logs', file_count: 0, total_size: 0 }))
    if (p === '/api/perf-metrics/summary') return fulfill(ok({ models: mode === 'empty' ? [] : models.map((model) => ({ model_name: model.model_name, avg_ttft_ms: 230, avg_latency_ms: 500, success_rate: 99.9, avg_tps: 80, cache_hit_rate: 40, input_tokens: 50000, cache_read_tokens: 20000, cache_write_tokens: 1000, request_count: 1200 })) }))
    if (p === '/api/perf-metrics') return fulfill(ok({ model_name: url.searchParams.get('model'), groups: [] }))
    if (p === '/api/marketplace/key-group-options') return fulfill(ok([{ value: 'marketplace_acceptance-group', label: 'Acceptance Route', category: 'marketplace', multiplier: 1, models: ['gpt-4.1'] }]))
    if (p === '/api/marketplace/auto-route-pool') return fulfill(ok({ token_group: 'auto', selected_count: 0, items: [], config: { strategy: 'priority', max_attempts: 3, failure_cooldown_seconds: 60, max_multiplier: 0 } }))
    if (p === '/api/user/topup/info') return fulfill(ok(topup))
    if (p === '/api/subscription/self') return fulfill(ok(subscription))
    if (p === '/api/user/models' || p === '/api/marketplace/models' || p === '/api/models/missing') return fulfill(ok(mode === 'empty' ? [] : models.map((m) => m.model_name)))
    if (p === '/api/user/self/groups') return fulfill(ok({ default: { desc: 'Default', ratio: 1 } }))
    if (p === '/api/group') return fulfill(ok(['default']))
    if (p === '/api/token/groups') return fulfill(ok([{ value: 'default', label: 'Default', type: 'system' }, { value: 'premium', label: 'Premium', type: 'system' }]))
    if (p === '/api/token' || p === '/api/token/search') return fulfill(ok(paginate(tokenRows)))
    if (p === '/api/token/1/models') return fulfill(ok(['gpt-4.1']))
    if (p === '/api/marketplace/groups') return fulfill(ok({ ...paginate(mode === 'empty' || url.searchParams.get('search') === 'no-such-market-item' ? [] : [group]), official_items: [], highlights: {}, ranked_count: 1, window_hours: 24 }))
    if (p === '/api/marketplace/group-status') return fulfill(ok(mode === 'empty' ? [] : [group]))
    if (p === '/api/marketplace/shops') return fulfill(ok(paginate(mode === 'empty' ? [] : [shop])))
    if (p === '/api/marketplace/shop/mine') return fulfill(ok(shop))
    if (p === '/api/marketplace/multiplier-trends') return fulfill(ok({ range_hours: 24, bucket_seconds: 3600, models: [], sources: [] }))
    if (/marketplace\/channels\/mine\/(logs|user-usage|user-multipliers|bargain-requests)/.test(p)) return fulfill(ok({ ...paginate(), summary: { request_count: 0, success_count: 0, failed_count: 0, consumer_amount: 0, owner_income: 0, pending_income: 0, released_income: 0, reclaimed_income: 0, total_users: 0, total_requests: 0 } }))
    if (p.endsWith('/observability')) return fulfill(ok({ items: [], start_timestamp: 0, end_timestamp: 0 }))
    if (/security-audit\/events/.test(p)) return fulfill(ok({ ...paginate(), summary: { total: 0, unreviewed: 0, affected_channels: 0, affected_users: 0, today: 0 } }))
    if (/marketplace\/admin\/(channels|reports|owner-income)/.test(p)) return fulfill(ok({ ...paginate(), owner_count: 0, request_count: 0, total_income: 0, pending_income: 0, released_income: 0, reclaimed_income: 0 }))
    if (p === '/api/wallet/transfers') return fulfill(ok({ quota_per_usd: 500000, min_quota: 500000, balance: user.quota, fee_bps: 100, security: { password_set: true, locked_until: 0, remaining_password_attempts: 5, requires_account_password: false, email_bound: true, email_masked: 'a***@example.test' }, history: paginate() }))
    if (p === '/api/wallet/unified-credit-migration') return fulfill(ok({ settlements: [] }))
    if (p === '/api/user/aff') return fulfill(ok('acceptance'))
    if (p === '/api/user/aff/overview') return fulfill(ok({ affiliate_code: 'acceptance', invited_count: 0, rebate_rate_bps: 500, rebate_effective_at: 0, first_week_consumed_quota: 0, rebate_total_quota: 0, reset_opportunity: resetOpportunity, invitees: [] }))
    if (p === '/api/user/2fa/status') return fulfill(ok({ enabled: false }))
    if (p === '/api/user/passkey') return fulfill(ok({ enabled: false, credentials: [] }))
    if (p === '/api/user/checkin') return fulfill(ok({ checkin_enabled: true, checked_in_today: false, records: [], checkin_count: 0, total_quota: 0 }))
    if (p === '/api/blind-box/self') return fulfill(ok({ enabled: true, unit_price: 1, daily_limit: 10, monthly_limit: 100, daily_open_limit: 10, count_options: [1, 5], tiers: [], pay_methods: topup.pay_methods, overview: { available_boxes: 0, pending_boxes: 0, quota: user.quota, pity_progress: 0, pity_threshold: 10, effective_pity_threshold: 10, purchased_today: 0, purchased_this_month: 0, recent_records: [] }, props: [], inventory: { enabled: mode !== 'empty', tiers: mode === 'empty' ? [] : [{ name: 'Fixture credit', min_usd: 10, max_usd: 20, probability: 0.9, reward_type: 'quota', wallet_type: 'default' }, { name: 'Fixture credit premium', min_usd: 50, max_usd: 100, probability: 0.1, reward_type: 'quota', wallet_type: 'default' }], inventory_count: mode === 'empty' ? 0 : 4, balance_usd: 100, price_usd: 15, purchased_today: 0, daily_purchase_limit: 100, remaining_purchase_limit: 100, pity_progress: 0, pity_threshold: 50, pity_guarantee_usd: 100, small_pity_progress: 0, small_pity_threshold: 10, small_pity_guarantee_usd: 20, first_draw_guarantee_usd: 15, first_draw_eligible: true, first_draw_reward_min_usd: 15, first_draw_reward_max_usd: 15, small_pity_reward_min_usd: 20, small_pity_reward_max_usd: 30, pity_reward_min_usd: 100, pity_reward_max_usd: 150 } }))
    if (p === '/api/blind-box/history') return fulfill(ok({ ...paginate(), records: [], retention_days: 30, cutoff_time: 0 }))
    if (p.includes('/log') && p.endsWith('/stat')) return fulfill(ok({ quota: 0, rpm: 0, tpm: 0 }))
    if (/^\/api\/(token|channel|user|models|vendors|redemption|deployments|task|log)(\/search)?$/.test(p) || p.includes('/topup') || /^\/api\/(log|task)\/self$/.test(p)) return fulfill(ok(paginate()))
    if (p === '/api/notice' || p === '/api/about' || p.startsWith('/api/home_page') || p === '/api/user-agreement' || p === '/api/privacy-policy') return fulfill(ok(''))
    if (p === '/api/desktop/auth/session') return fulfill(ok({ session_id: 'acceptance-session', user_code: 'ACCEPT', device_name: 'Acceptance Desktop', platform: 'windows', status: 'pending', created_at: 1792051200, expires_at: 4102444800 }))
    if (/^\/api\/(data|uptime|packages|subscription\/admin\/plans)|groups\/options|\/group-overview|\/channels\/mine$|\/multiplier-notices$|\/route-pools$|\/oauth\/bindings$|\/desktop\/devices$|\/custom-oauth|\/checkin|\/prefill_group|\/ratio_sync\/channels/.test(p)) return fulfill(ok([]))
    unknown.add(p)
    event.fixture = 'unmapped'
    return fulfill({ success: false, message: `Unmapped acceptance fixture: ${p}` }, 501)
  })
  return context
}

async function collectPage(page) {
  return page.evaluate(() => {
    const visible = (el) => { const rect = el.getBoundingClientRect(); const style = getComputedStyle(el); return rect.width > 0 && rect.height > 0 && style.visibility !== 'hidden' && style.display !== 'none' && style.opacity !== '0' && !el.closest('[hidden],[aria-hidden="true"]') }
    const text = document.body.innerText
    const width = document.documentElement.clientWidth
    const descriptions = [...document.querySelectorAll('p,[data-slot$="description"]')].filter((el) => visible(el) && el.getBoundingClientRect().width > 2 && el.getBoundingClientRect().height > 2 && !el.classList.contains('sr-only')).map((el) => ({ text: el.innerText?.trim(), tag: el.tagName, slot: el.getAttribute('data-slot'), documentContent: Boolean(el.closest('article,.prose,.markdown-body,.reading-content,.guide-content,.guide-sections')), configurationConstraint: /^Markdown\s*\/\s*HTML\s*\/\s*URL\s*·\s*(?:Empty:\s*disabled|留空：禁用)$|SQLite.*(?:持久|persist)|^订阅套餐创建和变更已锁定，管理员需先在支付设置中确认合规声明。$|^请先在系统设置中启用 io.net 模型部署服务，并配置 API Key。$/.test(el.innerText?.trim()) })).filter((el) => el.text?.length > 25 || (el.slot && el.text?.length > 0))
    const pageHeadings = [...document.querySelectorAll('h1')].filter((el) => { const style = getComputedStyle(el); const rect = el.getBoundingClientRect(); return rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden' })
    return { title: document.title, textLength: text.length, text: text.slice(0, 8000), h1Count: pageHeadings.length, accessibleH1Count: [...document.querySelectorAll('h1')].filter(visible).length, headings: [...document.querySelectorAll('h1,h2')].filter(visible).map((el) => el.textContent.trim()), actions: [...document.querySelectorAll('button,a,input,textarea,select')].filter(visible).length, horizontalOverflow: document.documentElement.scrollWidth - width, overflowElements: [...document.querySelectorAll('body *')].filter((el) => { if (!visible(el)) return false; const r = el.getBoundingClientRect(); return r.right > width + 2 && r.width > 10 && !el.closest('[data-slot="table-container"],[data-slot="scroll-area-viewport"]') }).slice(0, 10).map((el) => ({ tag: el.tagName, class: el.className?.baseVal ?? el.className, text: el.textContent.slice(0, 80), right: Math.round(el.getBoundingClientRect().right) })), brokenImages: [...document.images].filter((img) => visible(img) && (!img.complete || img.naturalWidth === 0)).map((img) => img.getAttribute('src')), forbiddenCopy: text.includes('不是'), descriptiveCopyCandidates: descriptions, fatalUI: /Something went wrong|Cannot read properties|Unexpected Application Error|Minified React error|(?:TypeError|ReferenceError|SyntaxError):|Invalid input: expected/.test(text) }
  })
}

function classifyCopy(candidate) {
  if (candidate.documentContent) return { allowed: true, purpose: 'preserved document content' }
  if (candidate.configurationConstraint) return { allowed: true, purpose: 'required configuration format or operational consequence' }
  const text = candidate.text
  if (text === '无效的重置链接，请请求新的密码重置。') return { allowed: true, purpose: 'invalid credential-reset state and recovery' }
  const configurationFormats = [
    /^(?:0–10|> 1 分钟|按小时聚合|user\+alias@domain\.com|RP ID = 当前域名或父域名|平台 \/ USB 安全密钥|HTTP · 仅开发环境|每行一个来源|GET \/api\/ratio|新 API 密钥 → auto groups\[0\]|8 = 80%|违规请求：额外收费 官方说明|扣费 = 基础金额 × 分组倍率|禁用：记录 \+ 转发|每行一条规则|关键词 \/ contains:短语 \/ re:正则|匹配 → Guard · 其他 → 上游|25 \/ 465 \/ 587|AUTH LOGIN|URL · 移除末尾斜杠|HTTP 图片代理|逐请求记录 · 数据库写入)$/,

    /^Waffo · API 密钥 · 商户 ID · RSA · 回调 URL$/,
    /^Waffo · 商户 ID · 商店 ID · 产品 ID · 私钥\nWebhook\n<ServerAddress>\/api\/waffo-pancake\/webhook$/,
    /^(?:OAuth \/ Webhook|HTML \/ URL → iframe|Markdown|-thinking → Anthropic 思考|0 = 永久|200, 429, 500–599|80, 443 · 留空：所有端口|不区分大小写 · 错误匹配：禁用渠道|固定为 USDT|关闭后用户端不再允许购买或开启|仅成功的请求|零费用模型：额度预扣|留空：保持原值|留空：服务器地址|每行一个 IP 或 CIDR 范围|每行一个域名|默认：5 个配额|默认值：USDTTRC20|失败 \+ 成功 · 0 = 无限制|微信支付最低费用CNY|已废弃 · 邀请奖励：刷新次数|已废弃 · 邀请钱包额度：已禁用|最低充值金额（美元）|DNS → IP 过滤|JSON \[\]|JSON \[model\]|JSON \{ model: headers \}|JSON \{\}|x-nowpayments-sig)$/,
    /^1 USD = \d+(?:\.\d+)?$/,
    /^CodeGo-(?:windows|macos|linux)\.(?:msi|dmg|AppImage) · \d+(?:\.\d+)? MB$/,
    /^可用空间: \d+(?:\.\d+)? \w+ \/ 总空间: \d+(?:\.\d+)? \w+$/,
    /^模型限流 · Web \/ API：环境配置 · HTTP 429$/,
    /^-thinking \/ -nothinking：保持原值$/,
    /^JSON \{ (?:category: safety|model: version) \} · 默认$/,
    /^-thinking \/ -thinking-\{\{budget\}\} \/ -nothinking$/,
    /^预算 = max_tokens × 比例 · (?:0\.002|0\.1)–1$/,
    /^Gemini \/ Vertex · OpenAI → thoughtSignature$/,
    /^Vertex AI · functionResponse\.id：已移除$/,
    /^示例 \{ "default": 8192, "claude-3-haiku-20240307": 4096, "claude-3-opus-20240229": 4096, "claude-3-7-sonnet-20250219-thinking": 8192 \}$/,
    /^10\.0\.0\.0\/8 · 172\.16\.0\.0\/12 · 192\.168\.0\.0\/16$/,
    /^200, 429, 500–599 已归一化： 100-199,300-399,401-407,409-503,505-523,525-599$/,
    /^IPN URL: \/api\/nowpayments\/ipn$/,
    /^支持 name、min_usd、max_usd、probability、reward_type 和 wallet_type，所有 probability 合计必须等于 1。这里不包含首购、小保底和大保底池。$/,
  ]
  if (text === '暂无数据') return { allowed: true, purpose: 'actual empty data state' }
  if (configurationFormats.some((pattern) => pattern.test(text))) return { allowed: true, purpose: 'required field syntax, numeric range or endpoint' }
  if (/^(?:确认前，支付、兑换码、订阅计划和邀请返利功能将保持锁定。|设置非零邀请奖励额度前，需要先在支付设置中确认合规声明。)/.test(text)) return { allowed: true, purpose: 'required legal consent and locked-feature consequence' }
  if (/^向其他用户转出通用额度。接收方收到完整金额，发送方另付 1% 手续费；月卡不可转账。$|^服务器会再次核对接收方、通用额度余额和 1% 手续费；转账完成后不可撤销。$/.test(text)) return { allowed: true, purpose: 'transaction fee, eligible balance and irreversible transfer consequence' }
  return { allowed: false, purpose: 'unreviewed visible explanatory copy' }
}

async function visit(page, route, viewport, mode) {
  const errors = []
  const listener = (error) => errors.push(String(error))
  page.on('pageerror', listener)
  const record = { route, viewport, mode, theme }
  try {
    const requests = contextRequests.get(page.context())
    const start = requests.length
    await page.goto(baseURL + (testURLs[route] || route), { waitUntil: 'domcontentloaded', timeout: 30000 })
    if (route === '/oauth/github') {
      await page.getByRole('heading', { level: 1 }).waitFor()
      record.callbackScreenshot = `${viewport}-oauth-callback.png`
      await page.screenshot({ path: path.join(output, record.callbackScreenshot), animations: 'disabled' })
    }
    await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {})
    await page.waitForTimeout(350)
    // Production queries retry transient 503 responses before their terminal UI.
    if (mode === 'error' && route.startsWith('/system-settings/')) await page.locator('.settings-workspace.settings-error h1').waitFor({ timeout: 15000 })
    if (route === '/wallet' && process.env.ACCEPTANCE_CLEAR_WALLET_AMOUNT === 'true') await page.locator('#topup-amount').fill('')
    if (callbackRoutes.has(route)) await page.waitForURL('**/dashboard/overview', { timeout: 7000 })
    Object.assign(record, await collectPage(page), { finalURL: page.url() })
    const final = new URL(page.url())
    const expectedPath = callbackRoutes.has(route) ? '/dashboard/overview' : aliases[route] || new URL(baseURL + route).pathname
    record.navigationKind = callbackRoutes.has(route) ? 'callback' : aliases[route] ? 'alias' : 'page'
    record.navigationValid = final.pathname.replace(/\/$/, '') === expectedPath.replace(/\/$/, '')
    if (route === '/invoices') record.navigationValid &&= final.searchParams.get('section') === 'invoices'
    if (route === '/oauth/github') record.navigationValid &&= requests.slice(start).some((request) => request.path === '/api/oauth/github' && request.search.includes('acceptance-code'))
    record.unknownFixtureEndpoints = [...new Set(requests.slice(start).filter((request) => unknown.has(request.path)).map((request) => request.path))]
    record.errorPageUnexpected = !/^\/(401|403|404|500|503|errors)(\/|$)/.test(route) && (/^\/(401|403|404|500|503)(\/|$)/.test(final.pathname) || record.headings.some((heading) => /^(401|403|404|500|503)$/.test(heading)))
    record.copyAssessments = record.descriptiveCopyCandidates.map((candidate) => ({ text: candidate.text, ...classifyCopy(candidate) }))
    record.copyReviewRequired = record.copyAssessments.some((item) => !item.allowed)
    record.stateValid = mode !== 'error' || /Acceptance API unavailable|加载失败|无法|出错|失败|不可用|重试|错误|unavailable|error|retry|failed/i.test(record.text)
    record.screenshot = `${viewport}-${mode}-${route.replace(/[^a-z0-9]+/gi, '-').replace(/^-|-$/g, '') || 'home'}.png`
    await page.screenshot({ path: path.join(output, record.screenshot), fullPage: process.env.ACCEPTANCE_FULL_PAGE === 'true', animations: 'disabled' })
    record.failures = [record.textLength <= 8 && 'blank-content', record.actions === 0 && 'no-actions', record.horizontalOverflow > 2 && 'horizontal-overflow', record.fatalUI && 'runtime-error-ui', record.forbiddenCopy && 'forbidden-copy', record.h1Count !== 1 && 'main-heading-count', errors.length && 'page-errors', !record.navigationValid && 'unexpected-navigation', record.errorPageUnexpected && 'unexpected-error-page', record.brokenImages.length && 'broken-images', record.copyReviewRequired && 'descriptive-copy-review', !record.stateValid && 'api-error-not-exposed', record.unknownFixtureEndpoints.length && 'unmapped-api-fixture'].filter(Boolean)
    record.pass = record.failures.length === 0
  } catch (error) { record.pass = false; record.error = String(error) }
  record.pageErrors = errors
  const previousIndex = results.findIndex((item) => item.route === route && item.viewport === viewport && item.mode === mode)
  if (previousIndex >= 0) results.splice(previousIndex, 1)
  results.push(record)
  page.off('pageerror', listener)
  writeQueue = writeQueue.then(() => fs.writeFile(path.join(output, 'page-results.json'), JSON.stringify(results, null, 2)))
  await writeQueue
  console.log(`${record.pass ? 'PASS' : 'FAIL'} ${viewport} ${mode} ${route} ${(record.failures || [record.error]).join(', ')}`)
}

async function interaction(name, run) {
  if (process.env.ACCEPTANCE_INTERACTIONS && !process.env.ACCEPTANCE_INTERACTIONS.split(',').some((prefix) => name.startsWith(prefix))) return
  const previousIndex = interactions.findIndex((item) => item.name === name)
  if (previousIndex >= 0) interactions.splice(previousIndex, 1)
  interactionRuntimeErrors = []
  try { const details = await run(); if (interactionRuntimeErrors.length) throw new Error(`Unhandled browser errors: ${interactionRuntimeErrors.join('; ')}`); interactions.push({ name, pass: true, details }); console.log(`PASS interaction ${name}`) }
  catch (error) {
    const artifacts = []
    for (const context of [...openContexts]) for (const page of context.pages()) {
      const filename = `failure-${name.replace(/[^a-z0-9]+/gi, '-').toLowerCase()}`
      try { const state = await collectPage(page); await fs.writeFile(path.join(output, `${filename}.json`), JSON.stringify(state, null, 2)); await page.screenshot({ path: path.join(output, `${filename}.png`), animations: 'disabled' }); artifacts.push(filename) } catch { /* page already closed */ }
    }
    interactions.push({ name, pass: false, error: String(error), pageErrors: interactionRuntimeErrors, artifacts }); console.log(`FAIL interaction ${name}: ${String(error).split('\n')[0]}`)
  }
  interactionRuntimeErrors = null
  for (const context of [...openContexts]) await context.close().catch(() => {})
  await fs.writeFile(path.join(output, 'interaction-results.json'), JSON.stringify(interactions, null, 2))
}

async function deletePlaygroundSession(page, row) {
  await row.getByRole('button', { name: /^删除 |^Delete / }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: /^确认$|^Confirm$/ }).click()
  await dialog.waitFor({ state: 'hidden' })
}

async function assertPlaygroundSend(page, message) {
  const input = page.getByRole('textbox', { name: '消息', exact: true })
  const before = allRequests.length
  await input.fill(message)
  if (await input.inputValue() !== message) throw new Error('Replacement session composer is not editable')
  await input.press('Enter')
  await page.getByText('Acceptance reply', { exact: true }).first().waitFor()
  const request = allRequests.slice(before).find((item) => ['/api/playground/chat/completions', '/v1/chat/completions', '/pg/chat/completions'].includes(item.path))
  if (!request || !JSON.parse(request.body).messages.some((item) => item.content === message)) throw new Error('Replacement session message was not sent')
  const stored = await page.evaluate(() => localStorage.getItem('codego:playground:v2:970001:sessions'))
  if (!stored?.includes(message)) throw new Error('Replacement session message was not persisted')
  return { actualRequest: { path: request.path, method: request.method, body: JSON.parse(request.body) }, messagePersisted: true }
}

try {
  if (!process.argv.includes('--interactions-only')) {
    const viewports = process.env.ACCEPTANCE_VIEWPORTS ? process.env.ACCEPTANCE_VIEWPORTS.split(',').map((width) => ({ width: Number(width), height: Number(width) === 375 ? 812 : 1024 })) : process.argv.includes('--quick') ? [{ width: 1440, height: 1000 }] : [{ width: 1440, height: 1000 }, { width: 375, height: 812 }, { width: 768, height: 1024 }]
    const selected = process.env.ACCEPTANCE_ROUTES ? process.env.ACCEPTANCE_ROUTES.split(',') : routeList
    await Promise.all(viewports.map(async (viewport) => {
      const context = await mockContext({ viewport })
      const page = await context.newPage()
      for (const route of selected) {
        if (/^\/(sign-in|sign-up|forgot-password|reset|otp|setup|user\/reset)(\/|$)/.test(route)) {
          const publicContext = await mockContext({ viewport, authenticated: false, setupReady: route !== '/setup' })
          const publicPage = await publicContext.newPage()
          await visit(publicPage, route, viewport.width, 'populated')
          await publicContext.close()
        } else await visit(page, route, viewport.width, 'populated')
      }
      await context.close()
    }))
    for (const mode of process.env.ACCEPTANCE_SKIP_STATES === 'true' ? [] : ['empty', 'error']) {
      for (const width of (process.env.ACCEPTANCE_STATE_VIEWPORTS || '1440').split(',').map(Number)) {
        const context = await mockContext({ mode, viewport: { width, height: width < 768 ? 844 : 1000 } })
        const page = await context.newPage()
        for (const route of ['/pricing', '/market', '/keys', '/dashboard/overview', '/usage-logs/common', '/wallet', '/blind-box', '/system-settings/site/system-info']) await visit(page, route, width, mode)
        await context.close()
      }
    }
  }
  if (!process.argv.includes('--pages-only')) {
  for (const route of ['/keys', '/wallet', '/system-settings/site/system-info']) {
    await interaction(`anonymous redirect ${route}`, async () => { const context = await mockContext({ authenticated: false }); const page = await context.newPage(); await page.goto(baseURL + route); await page.waitForURL('**/sign-in**'); const url = page.url(); await context.close(); return { url } })
  }
  await interaction('member settings permission redirect', async () => { const context = await mockContext({ role: 1 }); const page = await context.newPage(); await page.goto(baseURL + '/system-settings/site/system-info'); await page.waitForURL('**/403'); const url = page.url(); await context.close(); return { url } })
  await interaction('invalid section resolves visibly', async () => { const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/system-settings/site/no-such-section'); await page.waitForTimeout(300); const body = await page.locator('body').innerText(); if (!/404|找不到|不存在|Not Found/i.test(body)) throw new Error('Invalid section did not expose a not-found state'); await context.close(); return { text: body.slice(-500) } })
  await interaction('pricing search filters and clears', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/pricing')
    const search = page.locator('main input').first(); await search.fill('no-such-pricing-item'); await page.waitForTimeout(400)
    const empty = await page.locator('body').innerText(); if (await page.getByText('gpt-4.1', { exact: true }).count()) throw new Error('Unmatched pricing model remains visible')
    await search.fill(''); await page.getByText('gpt-4.1', { exact: true }).first().waitFor(); await context.close(); return { unmatchedText: empty.slice(-400), cleared: true }
  })
  await interaction('market search reaches API and clears', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/market')
    await page.getByRole('button', { name: 'Acceptance Route', exact: true }).waitFor()
    const start = allRequests.length; const search = page.getByPlaceholder(/搜索商品、店铺、模型或标签/); await search.fill('no-such-market-item'); await page.waitForTimeout(700)
    const requested = allRequests.slice(start).some((request) => request.path === '/api/marketplace/groups' && request.search.includes('no-such-market-item'))
    if (!requested) throw new Error('Search did not issue the filtered marketplace request')
    if (await page.getByRole('button', { name: 'Acceptance Route', exact: true }).count()) throw new Error('Empty search kept the market row')
    await search.fill(''); await page.getByRole('button', { name: 'Acceptance Route', exact: true }).waitFor(); await context.close(); return { filteredRequest: true, cleared: true }
  })
  await interaction('market group expansion', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/market')
    const toggle = page.getByRole('button', { name: 'Acceptance Route', exact: true }); await toggle.click()
    if (await toggle.getAttribute('aria-pressed') !== 'true') throw new Error('Group expansion selection missing')
    await page.screenshot({ path: path.join(output, 'market-group-subview.png'), animations: 'disabled' })
    const expanded = await collectPage(page); await toggle.click(); if (await toggle.getAttribute('aria-pressed') !== 'false') throw new Error('Group expansion did not close')
    await context.close(); return { expanded: expanded.text.slice(-1500), horizontalOverflow: expanded.horizontalOverflow }
  })
  await interaction('API key validation and successful mocked creation', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/keys')
    await page.getByRole('button', { name: /创建 API 密钥|创建密钥|Create API Key/i }).first().click()
    const dialog = page.getByRole('dialog'); await dialog.waitFor(); const start = allRequests.length
    await dialog.getByRole('button', { name: /保存更改|保存修改|Save changes/i }).click(); await page.waitForTimeout(200)
    if (allRequests.slice(start).some((request) => request.path === '/api/token' && request.method === 'POST')) throw new Error('Empty key name reached API')
    if (!await dialog.locator('[aria-invalid="true"]').count()) throw new Error('Key form did not mark invalid field')
    await dialog.locator('input[name="name"]').fill('Acceptance Created Key')
    await dialog.getByRole('button', { name: /保存更改|保存修改|Save changes/i }).click(); await dialog.waitFor({ state: 'hidden' })
    const request = allRequests.slice(start).find((request) => request.path === '/api/token' && request.method === 'POST'); if (!request) throw new Error('Valid key creation did not reach API')
    await context.close(); return { validationBlockedRequest: true, createdPayload: JSON.parse(request.body) }
  })
  await interaction('API key editing detail loading failure retry and rejected draft', async () => {
    const keyDetails = { group: 'premium', allow_ips: '10.20.30.0/24', model_limits_enabled: true, model_limits: 'gpt-4.1', remain_quota: 2500000, cross_group_retry: true, marketplace_multiplier_limit: 1.3 }
    const control = { keyDetails, keyDetailDelay: 1200, keyDetailFailure: true, reject: ({ path, method }) => path === '/api/token' && method === 'PUT' }
    const context = await mockContext({ control }); const page = await context.newPage(); await page.goto(baseURL + '/keys')
    await page.getByRole('button', { name: /打开菜单|Open menu/i }).first().click(); await page.getByRole('menuitem', { name: /编辑|Edit/i, exact: true }).click()
    const dialog = page.getByRole('dialog'); const save = dialog.getByRole('button', { name: /保存更改|保存修改|Save changes/i }); await save.waitFor()
    if (!await save.isDisabled() || await dialog.locator('input[name="name"]').isVisible()) throw new Error('Edit allowed submission before full detail arrived')
    if (contextRequests.get(context).some((r) => r.path === '/api/token' && r.method === 'PUT')) throw new Error('Detail loading issued a write')
    await dialog.getByText('Acceptance detail rejected', { exact: true }).waitFor()
    if (!await save.isDisabled()) throw new Error('Failed detail enabled save')
    control.keyDetailDelay = 0; control.keyDetailFailure = false
    await dialog.getByRole('button', { name: /重试|Retry/i }).click(); const input = dialog.locator('input[name="name"]'); await input.waitFor()
    if (await input.inputValue() !== key.name) throw new Error('Retry did not hydrate full token details')
    await input.fill('Rejected edit draft'); await save.click(); await page.getByText('Acceptance mutation rejected', { exact: true }).first().waitFor()
    if (!await dialog.isVisible() || await input.inputValue() !== 'Rejected edit draft' || control.tokenRows[0].name !== key.name) throw new Error('Rejected edit lost the draft or modified the row')
    control.reject = undefined; await save.click(); await dialog.waitFor({ state: 'hidden' }); await page.getByText('Rejected edit draft', { exact: true }).waitFor()
    const writes = contextRequests.get(context).filter((r) => r.path === '/api/token' && r.method === 'PUT')
    if (writes.length !== 2 || JSON.parse(writes[1].body).id !== 1) throw new Error('Edit did not send exactly the rejected and retried payloads')
    const savedPayload = JSON.parse(writes[1].body)
    for (const [field, value] of Object.entries(keyDetails)) if (savedPayload[field] !== value) throw new Error(`Editing the name changed saved token field ${field}: ${JSON.stringify(savedPayload[field])}`)
    await context.close(); return { fullDetailsRequired: true, detailRetry: true, rejectedDraftPreserved: true, savedRestrictionsPreserved: true, savedPayload }
  })
  await interaction('API key status and deletion rejection preserve row', async () => {
    const control = { reject: ({ path, method }) => (path === '/api/token' && method === 'PUT') || method === 'DELETE' }
    const context = await mockContext({ control }); const page = await context.newPage(); await page.goto(baseURL + '/keys')
    await page.getByRole('button', { name: /^(禁用|Disable)$/ }).first().click(); await page.getByText('Acceptance mutation rejected', { exact: true }).first().waitFor()
    if (control.tokenRows[0].status !== 1 || !await page.getByRole('button', { name: /^(禁用|Disable)$/ }).first().isVisible()) throw new Error('Rejected disable changed token state')
    control.reject = ({ method }) => method === 'DELETE'; await page.getByRole('button', { name: /^(禁用|Disable)$/ }).first().click(); await page.getByRole('button', { name: /^(启用|Enable)$/ }).first().waitFor()
    if (control.tokenRows[0].status !== 2) throw new Error('Successful disable was not persisted')
    await page.getByRole('button', { name: /打开菜单|Open menu/i }).first().click(); await page.getByRole('menuitem', { name: /^(删除|Delete)$/ }).click()
    const dialog = page.getByRole('alertdialog'); await dialog.getByRole('button', { name: /^(删除|Delete)$/ }).click(); await page.waitForTimeout(300)
    if (control.tokenRows.length !== 1 || !await page.getByText(key.name, { exact: true }).first().isVisible()) throw new Error('Rejected delete removed token row')
    const rejectionDialogPreserved = await dialog.isVisible()
    control.reject = undefined
    if (!rejectionDialogPreserved) { await page.getByRole('button', { name: /打开菜单|Open menu/i }).first().click(); await page.getByRole('menuitem', { name: /^(删除|Delete)$/ }).click() }
    await dialog.getByRole('button', { name: /^(删除|Delete)$/ }).click(); await dialog.waitFor({ state: 'hidden' }); await page.getByText(key.name, { exact: true }).waitFor({ state: 'hidden' })
    if (control.tokenRows.length) throw new Error('Successful delete did not remove row')
    await context.close(); return { rejectedDisablePreserved: true, disabled: true, rejectedDeletePreserved: true, rejectionDialogPreserved, deleted: true }
  })
  await interaction('market route pool create ordering rejection retry and delete', async () => {
    const control = { poolMembers: true, reject: ({ path, method }) => path === '/api/marketplace/route-pools/acceptance-pool' && ['PUT', 'DELETE'].includes(method) }
    const context = await mockContext({ control }); const page = await context.newPage(); await page.goto(baseURL + '/market')
    const panel = page.locator('.poolpanel'); await panel.locator('select.iname').selectOption('acceptance-pool'); await panel.locator('.pool-item').filter({ hasText: 'Second Route' }).waitFor()
    await panel.locator('.pool-item').first().getByTitle('下移', { exact: true }).click()
    const draftOrder = await panel.locator('.pool-item .cname').allTextContents(); if (draftOrder[0] !== 'Second Route') throw new Error('Pool order did not change locally')
    const save = panel.getByRole('button', { name: '保存更改', exact: true }); await save.click(); await page.getByText('Acceptance mutation rejected', { exact: true }).first().waitFor()
    if (!await save.isVisible() || (await panel.locator('.pool-item .cname').allTextContents())[0] !== 'Second Route' || control.poolRows[0].items[0].group_id !== group.id) throw new Error('Rejected pool order lost draft or changed persisted order')
    control.reject = ({ method }) => method === 'DELETE'; await save.click(); await save.waitFor({ state: 'hidden' })
    if (control.poolRows[0].items[0].group_id !== 'second-group') throw new Error('Pool order save omitted member order')
    await panel.getByTitle('删除池', { exact: true }).click(); await page.waitForTimeout(250)
    if (control.poolRows.length !== 1 || await panel.locator('select.iname').inputValue() !== 'acceptance-pool') throw new Error('Rejected pool deletion removed current pool')
    control.reject = undefined; await panel.getByTitle('删除池', { exact: true }).click(); await page.waitForTimeout(250)
    if (control.poolRows.length !== 0) throw new Error('Pool deletion did not persist')
    await panel.getByTitle('新建路由池', { exact: true }).click(); await panel.getByPlaceholder('高峰保障池').fill('Acceptance Created Pool')
    control.reject = ({ path, method }) => path === '/api/marketplace/route-pools' && method === 'POST'; await panel.getByRole('button', { name: '创建', exact: true }).click(); await panel.getByText('Acceptance mutation rejected', { exact: true }).waitFor()
    if (await panel.getByPlaceholder('高峰保障池').inputValue() !== 'Acceptance Created Pool' || control.poolRows.length) throw new Error('Rejected creation lost name or created pool')
    control.reject = undefined; await panel.getByRole('button', { name: '创建', exact: true }).click(); await panel.getByRole('heading', { name: 'Acceptance Created Pool', exact: true }).waitFor()
    const requests = contextRequests.get(context).filter((r) => r.path.includes('/route-pools') && r.method !== 'GET').map((r) => ({ path: r.path, method: r.method, body: r.body && JSON.parse(r.body) }))
    await context.close(); return { rejectedOrderDraftPreserved: true, ordered: true, rejectedDeletePreserved: true, deleted: true, rejectedCreateDraftPreserved: true, created: true, requests }
  })
  await interaction('wallet transfer invalid cancel and rejection preserve form', async () => {
    const control = { reject: ({ path, method }) => path === '/api/wallet/transfers' && method === 'POST' }
    const context = await mockContext({ control }); const page = await context.newPage(); await page.goto(baseURL + '/wallet?section=transfer')
    const recipient = page.locator('#wallet-transfer-recipient'); await recipient.fill('ABC123'); await page.getByRole('button', { name: /查找收款人|Find recipient/ }).click(); await page.getByText('A*** · ABC123', { exact: true }).waitFor()
    const amount = page.locator('#wallet-transfer-amount'); const review = page.getByRole('button', { name: /核对转账|Review transfer/ }); const invalid = []
    for (const value of ['', '-1', '0', '0.50', '1000']) { await amount.fill(value); if (!await review.isDisabled()) throw new Error(`Invalid transfer amount ${value} enabled review`); invalid.push(value) }
    const mutations = () => contextRequests.get(context).filter((r) => r.path === '/api/wallet/transfers' && r.method === 'POST')
    if (mutations().length) throw new Error('Invalid transfer amount issued mutation')
    await amount.fill('10'); await review.click(); const dialog = page.getByRole('dialog'); await dialog.getByRole('button', { name: /^(取消|Cancel)$/ }).click(); await dialog.waitFor({ state: 'hidden' })
    if (mutations().length || await amount.inputValue() !== '10' || await recipient.inputValue() !== 'ABC123') throw new Error('Cancel issued transfer or discarded draft')
    await review.click(); await dialog.locator('#transfer-payment-password').fill('acceptance-password'); await dialog.getByRole('button', { name: /立即转账|Transfer now/ }).click(); await page.getByText('Acceptance mutation rejected', { exact: true }).first().waitFor()
    await amount.waitFor()
    if (!await dialog.isVisible() || await amount.inputValue() !== '10' || await recipient.inputValue() !== 'ABC123') throw new Error('Rejected transfer lost recipient amount or confirmation')
    const passwordClearedOnRejection = await dialog.locator('#transfer-payment-password').inputValue() === ''
    if (mutations().length !== 1 || JSON.parse(mutations()[0].body).amount_quota !== 5000000) throw new Error('Transfer request did not match entered amount')
    await context.close(); return { invalidValuesBlocked: invalid, cancelDidNotTransfer: true, rejectedDraftPreserved: true, passwordClearedOnRejection, request: JSON.parse(mutations()[0].body), settlement: 'unverified; rejection fixture' }
  })
  await interaction('wallet payment cancel and mocked rejection', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/wallet')
    await page.getByRole('button', { name: /Acceptance Pay/ }).first().click()
    const dialog = page.getByRole('alertdialog'); await dialog.waitFor(); const beforeCancel = allRequests.filter((r) => r.path.endsWith('/pay')).length
    await dialog.getByRole('button', { name: /取消|Cancel/i }).click(); await dialog.waitFor({ state: 'hidden' })
    if (allRequests.filter((r) => r.path.endsWith('/pay')).length !== beforeCancel) throw new Error('Cancel issued payment')
    await page.getByRole('button', { name: /Acceptance Pay/ }).first().click(); await dialog.getByRole('button', { name: /确认付款|确认支付|Confirm Payment/i }).click()
    await page.getByText('Acceptance payment rejected', { exact: true }).first().waitFor()
    await page.screenshot({ path: path.join(output, 'wallet-mocked-payment-rejection.png'), animations: 'disabled' })
    await context.close(); return { cancelDidNotPay: true, rejectionDisplayed: true, settlement: 'not tested; mocked rejection only' }
  })
  await interaction('settings mocked save and reload', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/system-settings/site/notice')
    await page.locator('textarea[name="Notice"]').fill('Acceptance notice')
    const start = allRequests.length; await page.getByRole('button', { name: /保存通知|Save notice/i }).click()
    await page.waitForTimeout(300)
    const request = allRequests.slice(start).find((r) => r.path === '/api/option' && r.method === 'PUT'); if (!request) throw new Error('Settings save did not reach option API')
    await page.reload(); await page.locator('textarea[name="Notice"]').waitFor(); if (await page.locator('textarea[name="Notice"]').inputValue() !== 'Acceptance notice') throw new Error('Mocked setting failed reload')
    await context.close(); return { savedPayload: JSON.parse(request.body), reloaded: true }
  })
  await interaction('settings partial save failure and external options preserve draft', async () => {
    let externalUpdateApplied = false
    const control = {
      reject: ({ path, body }) => path === '/api/option' && body.key === 'PasswordRegisterEnabled',
      afterOptionWrite: (body, options) => { if (body.key === 'PasswordLoginEnabled') { options.push({ key: 'EmailDomainWhitelist', value: 'external.example.test' }); externalUpdateApplied = true } },
    }
    const context = await mockContext({ control }); const page = await context.newPage(); await page.goto(baseURL + '/system-settings/auth/basic-auth')
    const login = page.getByRole('switch', { name: /^(密码登录|Password Login)$/ }); const register = page.getByRole('switch', { name: /^(密码注册|Password Registration)$/ }); const whitelist = page.locator('textarea[name="EmailDomainWhitelist"]')
    await login.waitFor(); const loginBefore = await login.getAttribute('aria-checked'); const registerBefore = await register.getAttribute('aria-checked')
    await login.click(); await register.click(); await whitelist.fill('draft.example.test'); await page.getByRole('button', { name: /保存更改|Save Changes/i }).click()
    await page.getByText('Acceptance mutation rejected', { exact: true }).first().waitFor(); await page.waitForTimeout(300)
    if (!externalUpdateApplied) throw new Error('First saved option did not cause external options update')
    if (await login.getAttribute('aria-checked') === loginBefore || await register.getAttribute('aria-checked') === registerBefore || await whitelist.inputValue() !== 'draft.example.test') throw new Error('Partial failure or external options update replaced user drafts')
    const firstWrites = contextRequests.get(context).filter((r) => r.path === '/api/option' && r.method === 'PUT').map((r) => JSON.parse(r.body))
    if (!firstWrites.some((r) => r.key === 'PasswordLoginEnabled') || !firstWrites.some((r) => r.key === 'PasswordRegisterEnabled') || firstWrites.some((r) => r.key === 'EmailDomainWhitelist')) throw new Error('Partial save did not stop at the rejected second option')
    control.reject = undefined; control.afterOptionWrite = undefined; await page.getByRole('button', { name: /保存更改|Save Changes/i }).click(); await page.waitForTimeout(500)
    await page.reload(); await whitelist.waitFor(); if (await whitelist.inputValue() !== 'draft.example.test' || await login.getAttribute('aria-checked') === loginBefore || await register.getAttribute('aria-checked') === registerBefore) throw new Error('Retried setting drafts did not survive reload')
    await context.close(); return { firstWrites, partialSavePreservedBothBooleans: true, externalWhitelistDidNotOverwriteDraft: true, retryAndReload: true }
  })
  await interaction('blind box purchase confirmation cancellation and cash rejection', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/blind-box')
    const purchase = page.getByRole('region', { name: '购买盲盒', exact: true })
    await purchase.getByRole('button', { name: /额度支付/ }).click()
    const confirmation = page.getByRole('dialog'); await confirmation.getByRole('heading', { name: '确认购买', exact: true }).waitFor()
    const start = allRequests.length; await confirmation.getByRole('button', { name: '取消', exact: true }).click(); await confirmation.waitFor({ state: 'hidden' })
    if (allRequests.slice(start).some((request) => /blind-box\/(inventory\/purchase|purchase)$/.test(request.path))) throw new Error('Cancelled credit purchase issued a mutation')
    await purchase.getByRole('button', { name: /现金支付/ }).click()
    const payment = page.getByRole('dialog'); await payment.getByText('Acceptance payment rejected', { exact: true }).first().waitFor()
    await payment.locator('[data-slot="dialog-close"]').click(); await payment.waitFor({ state: 'hidden' })
    const inventory = page.locator('.blind-box-inventory-surface'); await inventory.getByRole('button', { name: '保底进度', exact: true }).click()
    await page.locator('.blind-box-secondary-panel').waitFor()
    const records = allRequests.slice(start).filter((request) => request.path === '/api/blind-box/pay')
    if (records.length !== 1) throw new Error('Cash rejection should issue exactly one fixture payment request')
    await page.screenshot({ path: path.join(output, 'blind-box-mocked-cash-rejection.png'), animations: 'disabled' })
    await context.close(); return { cancelledCreditPurchase: true, mockedCashRejection: true, pityControls: true }
  })
  await interaction('playground session create rename filter persist and send', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/playground'); await page.locator('.pg-new-session').click()
    await page.locator('.pg-session-actions button').first().click(); const dialog = page.getByRole('dialog'); await dialog.locator('#pg-session-name').fill('Acceptance Session')
    await dialog.getByRole('button', { name: /保存|Save/i }).click(); await dialog.waitFor({ state: 'hidden' })
    await page.getByRole('textbox', { name: '搜索会话', exact: true }).fill('no-such-session'); await page.getByText('无匹配会话', { exact: true }).waitFor()
    await page.getByRole('textbox', { name: '搜索会话', exact: true }).fill('Acceptance'); await page.locator('.pg-session-list').getByText('Acceptance Session', { exact: true }).waitFor()
    await page.reload(); await page.locator('.pg-session-list').getByText('Acceptance Session', { exact: true }).waitFor()
    const input = page.getByRole('textbox', { name: '消息', exact: true }); const start = allRequests.length; await input.fill('Acceptance ping'); await input.press('Enter'); await page.getByText('Acceptance reply', { exact: true }).first().waitFor()
    const request = allRequests.slice(start).find((item) => item.path === '/api/playground/chat/completions' || item.path === '/v1/chat/completions' || item.path === '/pg/chat/completions')
    if (!request || !JSON.parse(request.body).messages.some((message) => message.content === 'Acceptance ping')) throw new Error('Conversation did not issue the actual message request')
    await page.screenshot({ path: path.join(output, 'playground-mocked-conversation.png'), animations: 'disabled' })
    const persisted = await page.evaluate(() => localStorage.getItem('codego:playground:v2:970001:sessions'))
    if (!persisted?.includes('Acceptance ping')) throw new Error('Conversation was not persisted')
    await context.close(); return { renamed: true, filter: true, persisted: true, mockedReply: true }
  })
  await interaction('playground delete current session activates editable remaining session', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/playground')
    await page.locator('.pg-new-session').click()
    if (await page.locator('.pg-session').count() !== 2) throw new Error('Expected two sessions before current deletion')
    await deletePlaygroundSession(page, page.locator('.pg-session[data-active="true"]'))
    if (await page.locator('.pg-session').count() !== 1 || await page.locator('.pg-session[data-active="true"]').count() !== 1) throw new Error('Remaining session did not become active')
    const details = await assertPlaygroundSend(page, 'After deleting current')
    await page.screenshot({ path: path.join(output, 'playground-delete-current.png'), animations: 'disabled' })
    await context.close(); return details
  })
  await interaction('playground delete final session creates editable replacement', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/playground')
    const oldId = await page.locator('.pg-session-select[aria-current="page"]').getAttribute('aria-current')
    const before = await page.evaluate(() => JSON.parse(localStorage.getItem('codego:playground:v2:970001:sessions') || '[]')[0]?.id)
    await deletePlaygroundSession(page, page.locator('.pg-session[data-active="true"]'))
    if (await page.locator('.pg-session').count() !== 1 || await page.locator('.pg-session[data-active="true"]').count() !== 1) throw new Error('Deleting the last session did not create an active replacement')
    const replacement = await page.evaluate(() => JSON.parse(localStorage.getItem('codego:playground:v2:970001:sessions') || '[]')[0]?.id)
    if (!replacement || before === replacement || !oldId) throw new Error('Final deletion retained the removed session')
    const details = await assertPlaygroundSend(page, 'After deleting final')
    await page.screenshot({ path: path.join(output, 'playground-delete-final.png'), animations: 'disabled' })
    await context.close(); return { replacedId: before, replacementId: replacement, ...details }
  })
  await interaction('playground delete noncurrent session preserves current draft', async () => {
    const context = await mockContext(); const page = await context.newPage(); await page.goto(baseURL + '/playground')
    await page.locator('.pg-new-session').click()
    const input = page.getByRole('textbox', { name: '消息', exact: true })
    await input.fill('Current draft survives')
    const activeBefore = await page.evaluate(() => document.querySelector('.pg-session[data-active="true"] .pg-session-select').innerText)
    await deletePlaygroundSession(page, page.locator('.pg-session[data-active="false"]'))
    if (await input.inputValue() !== 'Current draft survives') throw new Error('Deleting another session replaced the active draft')
    const activeAfter = await page.evaluate(() => document.querySelector('.pg-session[data-active="true"] .pg-session-select').innerText)
    if (activeBefore !== activeAfter) throw new Error('Deleting another session replaced the active selection')
    const details = await assertPlaygroundSend(page, 'Current draft survives')
    await page.screenshot({ path: path.join(output, 'playground-delete-noncurrent.png'), animations: 'disabled' })
    await context.close(); return { draftPreserved: true, ...details }
  })
  }
} finally {
  await writeQueue
  await fs.writeFile(path.join(output, 'page-results.json'), JSON.stringify(results, null, 2))
  await fs.writeFile(path.join(output, 'api-requests.json'), JSON.stringify(allRequests, null, 2))
  const viewportCoverage = [375, 768, 1440].map((viewport) => ({ viewport, expectedRoutes: routeList.length, checkedRoutes: new Set(results.filter((result) => result.viewport === viewport && result.mode === 'populated').map((result) => result.route)).size, missingRoutes: routeList.filter((route) => !results.some((result) => result.viewport === viewport && result.mode === 'populated' && result.route === route)), settingsChecked: sections.filter((route) => results.some((result) => result.viewport === viewport && result.mode === 'populated' && result.route === route)).length }))
  const designCoverage = designObjects.map((object) => ({ ...object, evidence: object.acceptance.startsWith('interaction: ') ? interactions.filter((result) => result.name === object.acceptance.slice(13)) : results.filter((result) => result.route === object.acceptance && result.mode === 'populated').map((result) => ({ viewport: result.viewport, pass: result.pass, failures: result.failures, screenshot: result.screenshot })), verified: object.acceptance.startsWith('interaction: ') ? interactions.some((result) => result.name === object.acceptance.slice(13) && result.pass) : [375, 768, 1440].every((viewport) => results.some((result) => result.route === object.acceptance && result.mode === 'populated' && result.viewport === viewport && result.pass)) }))
  const criticalActions = [
    ['blind box cancellation and rejected cash payment', 'blind box'], ['authentication guard', 'anonymous redirect'], ['authorization guard', 'member settings'], ['invalid navigation', 'invalid section'], ['pricing filter', 'pricing search'], ['market server search', 'market search'], ['group details', 'market group expansion'], ['key lifecycle and rejected edits', 'API key'], ['route pool lifecycle and rejected writes', 'market route pool'], ['payment cancellation and rejection', 'wallet payment'], ['transfer validation cancellation and rejection', 'wallet transfer'], ['settings save partial failure and reload', 'settings'], ['playground persistence sending and deletion boundaries', 'playground'],
  ].map(([category, prefix]) => ({ category, checks: interactions.filter((item) => item.name.startsWith(prefix)).map((item) => ({ name: item.name, pass: item.pass })), pass: interactions.some((item) => item.name.startsWith(prefix)) && interactions.filter((item) => item.name.startsWith(prefix)).every((item) => item.pass) }))
  await fs.writeFile(path.join(output, 'summary.json'), JSON.stringify({ label, baseURL, finishedAt: new Date().toISOString(), scope: 'Local real Chromium with intercepted API fixtures. Production, real settlement, WebAuthn device authorization and upstream provider response are unverified. Render checks do not prove every page action; only named criticalActions were exercised.', routes: routeList.length, settingsSections: sections.length, designObjects: designObjects.length, viewportCoverage, designCoverage, criticalActions, pageChecks: results.length, pagePass: results.filter((r) => r.pass).length, pageFail: results.filter((r) => !r.pass).length, interactions, unknownFixtureEndpoints: [...unknown], failures: results.filter((r) => !r.pass).map(({ route, viewport, mode, horizontalOverflow, pageErrors, failures, unknownFixtureEndpoints, error, screenshot }) => ({ route, viewport, mode, horizontalOverflow, pageErrors, failures, unknownFixtureEndpoints, error, screenshot })) }, null, 2))
  for (const context of [...openContexts]) await context.close().catch(() => {})
  await browser.close()
}
console.log(JSON.stringify({ output, routes: routeList.length, sections: sections.length, pages: results.length, failures: results.filter((r) => !r.pass).length, interactions: interactions.length, interactionFailures: interactions.filter((r) => !r.pass).length }))
if (results.some((r) => !r.pass) || interactions.some((r) => !r.pass)) process.exitCode = 1
