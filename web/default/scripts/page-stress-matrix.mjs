/*
Copyright (C) 2023-2026 QuantumNous
SPDX-License-Identifier: AGPL-3.0-or-later
*/
import fs from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const repo = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../..'
)
const output = path.join(repo, 'output/playwright/page-stress')
const input = JSON.parse(
  await fs.readFile(
    path.join(repo, 'output/playwright/wallet-redesign/route-scan-input.json'),
    'utf8'
  )
)
const excluded = ['/oauth', '/desktop/authorize', '/setup']
const routes = input.routes.filter((route) => !excluded.includes(route.url))
routes.push({
  url: '/pricing/gpt-4o',
  kind: 'public',
  source: 'src/routes/pricing/$modelId.tsx',
})
routes.push({
  url: '/oauth',
  kind: 'authentication',
  source: 'src/routes/oauth/index.tsx',
  renderOnly: true,
})
routes.push({
  url: '/desktop/authorize?session_id=synthetic-session&code=SYNTHETIC',
  kind: 'console',
  source: 'src/routes/desktop/authorize.tsx',
  renderOnly: true,
})
routes.push({
  url: '/setup',
  kind: 'public',
  source: 'src/routes/setup/index.tsx',
  renderOnly: true,
})
const fixed = (
  apis = [],
  boundary = 'Fixed structure; long text, 390px viewport, loading and error states'
) => ({
  apis,
  pagination: 'not applicable',
  scales: ['fixed structure'],
  boundary,
})
const full = (
  apis,
  boundary = '1000 / 10000 / 50000 records; long labels and large finite values'
) => ({
  apis,
  pagination: 'full response; inspect actual rendered item count separately',
  scales: [1000, 10000, 50000],
  boundary,
})
const paged = (apis, size = '10 / 20 / 100') => ({
  apis,
  pagination: `server pagination; retain requested page size ${size}; totals scale to 50000`,
  scales: [1000, 10000, 50000],
  boundary:
    'First / middle / final / out-of-range pages; empty / 503 / 1500ms latency; long labels and large finite values',
})
function classify(route) {
  const url = new URL(route, 'http://local')
  const p = url.pathname
  if (p === '/setup')
    return fixed(
      ['/api/setup'],
      'Read-only initial setup form using synthetic status=false; no initialization submission'
    )
  if (p === '/desktop/authorize')
    return fixed(
      ['/api/desktop/auth/session'],
      'Synthetic pending session; show device and permissions; no approval or rejection'
    )
  if (p === '/oauth')
    return fixed(
      [],
      'Missing callback parameters fallback; no provider token exchange'
    )
  if (p.startsWith('/system-settings'))
    return fixed(
      ['/api/option/', '/api/status'],
      'Fixed settings sections; large serialized text/config, pending and 503; no synthetic thousands of form controls'
    )
  if (p === '/' || p === '/pricing' || p.startsWith('/pricing/'))
    return full(
      ['/api/pricing', '/api/packages/public'],
      'Full catalogue 1k/10k/50k; home six-card page; pricing catalogue actual per-page render; detail lookup and large model strings'
    )
  if (p === '/marketplace') return paged(['/api/marketplace/admin/channels'])
  if (['/market', '/rankings'].includes(p))
    return {
      ...paged(['/api/marketplace/groups', '/api/marketplace/shops']),
      additionalFullResponse: [
        '/api/marketplace/models',
        '/api/pricing',
        '/api/perf-metrics/summary',
      ],
      boundary:
        'Preserve server page size; leaderboard limited separately; all-model selector 1k/10k/50k; 503 / slow / empty',
    }
  if (p === '/group-status')
    return full(
      ['/api/group-status', '/api/user/self/group-status'],
      'All groups 1k/10k/50k; actual card and status history rendering; empty / 503 / slow'
    )
  if (p === '/channel-workbench')
    return {
      ...full(['/api/marketplace/channels/mine']),
      additionalPagedResponses: [
        '/api/marketplace/channels/mine/logs',
        '/api/marketplace/channels/mine/user-usage',
      ],
      boundary:
        'All owned channels 1k/10k/50k; income/users/logs retain real pagination; inspect progressive-load requests and DOM growth',
    }
  if (p.startsWith('/dashboard'))
    return full(
      [
        '/api/data/self',
        '/api/data',
        '/api/data/users',
        '/api/perf-metrics/summary',
      ],
      '1k/10k/50k aggregate records across 100 models/users; chart aggregation and timeline bounds; large finite balances; sequential log fanout measured separately'
    )
  if (['/usage-logs', '/usage-logs/common', '/console/log'].includes(p))
    return paged([
      '/api/log/',
      '/api/log/self',
      '/api/log/stat',
      '/api/log/self/stat',
    ])
  if (p === '/usage-logs/task') return paged(['/api/task', '/api/task/self'])
  if (p === '/keys')
    return {
      ...paged(['/api/token/', '/api/token/search']),
      additionalFullResponse: ['/api/user/models', '/api/token/groups'],
    }
  if (p === '/channels')
    return {
      ...paged(['/api/channel', '/api/channel/search']),
      additionalFullResponse: [
        '/api/channel/models',
        '/api/marketplace/route-pools',
      ],
    }
  if (p === '/users') return paged(['/api/user/', '/api/user/search'])
  if (p === '/redemption-codes')
    return paged(['/api/redemption/', '/api/redemption/search'])
  if (p === '/models' || p === '/models/metadata')
    return {
      ...paged(['/api/models/', '/api/models/search', '/api/vendors/']),
      boundary:
        'Model metadata server-page 10/20/100; vendor selector follows sequential 100-row pages and backend 100 cap; total 1k/10k/50k',
    }
  if (p === '/models/deployments')
    return paged(['/api/deployments/', '/api/deployments/search'])
  if (p === '/blind-box')
    return {
      ...paged(['/api/blind-box/self', '/api/blind-box/history']),
      fixedResponses: ['/api/blind-box/amount'],
      boundary:
        'History 1k/10k/50k paged; inventory extreme finite counts with bounded preview; no buy/open/prop mutation; quote 503 and race covered separately',
    }
  if (
    p === '/invoices' ||
    (p === '/wallet' && url.searchParams.get('section') === 'invoices')
  )
    return {
      ...full(
        ['/api/invoices/eligible-orders'],
        'Eligible orders full-response 1k/10k/50k; client pagination and global search; no invoice download/issue mutation'
      ),
      additionalPagedResponses: [
        '/api/invoices/requests',
        '/api/invoices/admin/requests',
      ],
    }
  if (p === '/wallet' || p === '/console/topup')
    return {
      ...fixed(
        [
          '/api/subscription/self',
          '/api/packages/public',
          '/api/user/topup/info',
        ],
        'Default fixed balance summary; histories open on demand, separately verified at 50000 total records; invalid amounts; no financial mutation'
      ),
      additionalPagedResponses: [
        '/api/user/topup/self',
        '/api/user/topup',
        '/api/wallet/transfers',
        '/api/invoices/requests',
      ],
    }
  if (p === '/invoices')
    return paged(['/api/invoices/requests', '/api/invoices/admin/requests'])
  if (p === '/security-audit')
    return paged([
      '/api/marketplace/admin/security-audit/events',
      '/api/marketplace/security-audit/events',
    ])
  if (['/packages', '/subscriptions', '/group-buy'].includes(p))
    return fixed(
      [
        '/api/packages/public',
        '/api/subscription/plans',
        '/api/subscription/self',
        '/api/group-buy/list',
      ],
      'Actual 5-plan commerce catalogue; large finite price/credit amounts and long product titles; empty / failed / slow; no fake 50000 simultaneous product cards'
    )
  if (p === '/images') return paged(['/api/user/image-workspace/items'])
  if (p === '/playground')
    return full(
      ['/api/user/models'],
      '1k/10k/50k available model options; input boundary and rendering only; no live inference or generated paid images'
    )
  if (p === '/chat2link')
    return fixed(
      ['/api/status'],
      'Configured client links are fixed navigation; no Web chat links configured in baseline; unavailable state tested'
    )
  if (p === '/invite-rewards')
    return full(
      ['/api/user/aff/overview'],
      'Invitation reward and invitee history contract has arrays; inspect actual DOM growth; no invitation or reward mutation'
    )
  if (p === '/profile')
    return fixed(
      ['/api/user/self', '/api/user/passkey', '/api/user/2fa/status'],
      'Long profile values; fixed account/security form; 503; no password/security modification'
    )
  if (p === '/operations')
    return fixed(
      ['/api/performance/stats', '/api/performance/logs'],
      'Large finite memory/counter values and long log directory names; loading / failure'
    )
  if (p === '/privacy-policy' || p === '/user-agreement')
    return fixed(
      [p === '/privacy-policy' ? '/api/privacy-policy' : '/api/user-agreement'],
      'Long 100KB markdown document; heading/list/code/table overflow; no fake list database'
    )
  if (
    p.startsWith('/topics') ||
    ['/docs', '/guide', '/faq', '/about', '/brand', '/download'].includes(p)
  )
    return fixed(
      ['/api/status'],
      'Fixed public content; long article/code block/list, 390px layout and navigation'
    )
  if (
    [
      '/sign-in',
      '/sign-up',
      '/forgot-password',
      '/reset',
      '/user/reset',
      '/otp',
    ].includes(p)
  )
    return fixed(
      ['/api/status'],
      'Fixed authentication form; long/invalid input, high latency, failed server response; no real registration/email/reset mutation'
    )
  return fixed(['/api/status'])
}
const matrix = routes.map((route) => ({ ...route, ...classify(route.url) }))
await fs.mkdir(output, { recursive: true })
await fs.writeFile(
  path.join(output, 'route-matrix.json'),
  JSON.stringify(
    {
      synthetic: true,
      renderOnlyRoutes: matrix
        .filter((route) => route.renderOnly)
        .map((route) => route.url),
      excludedMutations: [
        'purchase',
        'transfer',
        'open blind box',
        'approve/reject desktop',
        'initialize setup',
        'OAuth token exchange',
        'real inference',
      ],
      routes: matrix,
    },
    null,
    2
  )
)
await fs.writeFile(
  path.join(output, 'route-matrix.csv'),
  [
    'url,kind,pagination,apis,scales,boundary',
    ...matrix.map((row) =>
      [
        row.url,
        row.kind,
        row.pagination,
        row.apis.join(' | '),
        row.scales.join(' / '),
        row.boundary,
      ]
        .map((value) => `"${String(value).replaceAll('"', '""')}"`)
        .join(',')
    ),
  ].join('\n')
)
console.log(
  JSON.stringify({
    routes: matrix.length,
    fullResponseRoutes: matrix.filter((row) =>
      row.pagination.startsWith('full')
    ).length,
    serverPagedRoutes: matrix.filter((row) =>
      row.pagination.startsWith('server')
    ).length,
    fixedStructureRoutes: matrix.filter(
      (row) => row.pagination === 'not applicable'
    ).length,
    output,
  })
)
