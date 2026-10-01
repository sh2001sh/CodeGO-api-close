import { QueryClient } from '@tanstack/react-query'
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Outlet,
  redirect,
  useRouter,
} from '@tanstack/react-router'
import { api, unwrap, APIError } from './lib/api'
import { keysOptions, sessionOptions, walletOptions, resourceOptions } from './lib/queries'
import { Shell } from './components/shell'
import { Button, ErrorMessage, Loading } from './components/ui'
import { useTranslation } from './lib/i18n'
import { boxHistoryOptions } from './features/box-history'
import { safeLocalReturn } from './lib/auth-navigation'

type AuthSearch = { returnTo?: string }
const authSearch = (search: AuthSearch): AuthSearch => ({
  returnTo: safeLocalReturn(search.returnTo),
})

export const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 30_000, retry: 1, refetchOnWindowFocus: false } },
})

function RouteError(props: { error: unknown }) {
  const { t } = useTranslation()
  const router = useRouter()
  const error = props.error instanceof Error ? props.error : new Error('服务暂时不可用')
  return (
    <section className="page">
      <h1>{t('页面暂时不可用')}</h1>
      <ErrorMessage error={error} />
      <Button onClick={() => void router.invalidate()}>{t('重试')}</Button>
    </section>
  )
}

const root = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: Outlet,
  errorComponent: RouteError,
  notFoundComponent: () => (
    <section className="page">
      <h1>404</h1>
    </section>
  ),
})
const index = createRoute({
  getParentRoute: () => root,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/dashboard' })
  },
})
const signIn = createRoute({
  getParentRoute: () => root,
  path: '/sign-in',
  validateSearch: authSearch,
  component: lazyRouteComponent(() => import('./pages/auth')),
})
const signUp = createRoute({
  getParentRoute: () => root,
  path: '/sign-up',
  validateSearch: authSearch,
  component: lazyRouteComponent(() => import('./pages/register')),
})
const oauthCallback = createRoute({
  getParentRoute: () => root,
  path: '/oauth/$provider',
  component: lazyRouteComponent(() => import('./pages/oauth-callback')),
})
const authenticated = createRoute({
  getParentRoute: () => root,
  id: '_authenticated',
  component: Shell,
  beforeLoad: async ({ context, location }) => {
    try {
      return { user: await context.queryClient.ensureQueryData(sessionOptions()) }
    } catch (error) {
      if (error instanceof APIError && error.status === 401)
        throw redirect({ to: '/sign-in', search: { returnTo: safeLocalReturn(location.href) } })
      throw error
    }
  },
})
const dashboard = createRoute({
  getParentRoute: () => authenticated,
  path: '/dashboard',
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(walletOptions()),
      context.queryClient.ensureQueryData(keysOptions()),
    ]),
  component: lazyRouteComponent(() => import('./pages/dashboard')),
})
const keys = createRoute({
  getParentRoute: () => authenticated,
  path: '/keys',
  loader: ({ context }) => context.queryClient.ensureQueryData(keysOptions()),
  component: lazyRouteComponent(() => import('./pages/keys')),
})
const wallet = createRoute({
  getParentRoute: () => authenticated,
  path: '/wallet',
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(walletOptions()),
      import('./features/commerce/subscription-values').then((module) =>
        context.queryClient.ensureQueryData(module.subscriptionConversionsOptions()),
      ),
      import('./features/commerce/refunds').then((module) =>
        context.queryClient.ensureQueryData(module.refundsOptions()),
      ),
      import('./features/commerce/subscription-actions').then((module) =>
        context.queryClient.ensureQueryData(module.subscriptionPreferenceOptions()),
      ),
      context.queryClient.ensureQueryData(
        resourceOptions('payment-methods', (signal) =>
          api.GET('/api/commerce/providers', { signal }).then(unwrap),
        ),
      ),
      context.queryClient.ensureQueryData(
        resourceOptions('plans', (signal) =>
          api.GET('/api/subscription/plans', { signal }).then((result) => unwrap(result)),
        ),
      ),
      context.queryClient.ensureQueryData(
        resourceOptions('subscriptions', (signal) =>
          api.GET('/api/subscription/self', { signal }).then((result) => unwrap(result)),
        ),
      ),
    ]),
  component: lazyRouteComponent(() => import('./pages/wallet')),
})
const logs = createRoute({
  getParentRoute: () => authenticated,
  path: '/usage-logs',
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      resourceOptions(
        'logs',
        (signal) =>
          api
            .GET('/api/log/self', { signal, params: { query: { page_size: 50 } } })
            .then((result) => unwrap(result)),
        ['', ''],
      ),
    ),
  component: lazyRouteComponent(() => import('./pages/logs')),
})
const orders = createRoute({
  getParentRoute: () => authenticated,
  path: '/orders',
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      resourceOptions(
        'orders',
        (signal) =>
          api
            .GET('/api/commerce/orders', { signal, params: { query: { limit: 50 } } })
            .then((result) => unwrap(result)),
        [false, ''],
      ),
    ),
  component: lazyRouteComponent(() => import('./pages/orders')),
})
const groupBuy = createRoute({
  getParentRoute: () => authenticated,
  path: '/group-buy',
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(
        resourceOptions(
          'groups',
          (signal) => api.GET('/api/group-buy/list', { signal }).then((result) => unwrap(result)),
          ['', false],
        ),
      ),
      context.queryClient.ensureQueryData(
        resourceOptions('orders', (signal) =>
          api.GET('/api/commerce/orders', { signal }).then((result) => unwrap(result)),
        ),
      ),
      context.queryClient.ensureQueryData(
        resourceOptions('plans', (signal) =>
          api.GET('/api/subscription/plans', { signal }).then((result) => unwrap(result)),
        ),
      ),
    ]),
  component: lazyRouteComponent(() => import('./pages/group-buy')),
})
const blindBox = createRoute({
  getParentRoute: () => authenticated,
  path: '/blind-box',
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(boxHistoryOptions()),
      context.queryClient.ensureQueryData(
        resourceOptions('boxes', (signal) =>
          api.GET('/api/blind-box/self', { signal }).then((result) => unwrap(result)),
        ),
      ),
    ]),
  component: lazyRouteComponent(() => import('./pages/blind-box')),
})
const community = createRoute({
  getParentRoute: () => authenticated,
  path: '/community',
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      resourceOptions(
        'community',
        (signal) =>
          api
            .GET('/api/community/sellers', {
              signal,
              params: { query: { page: 1, page_size: 20 } },
            })
            .then((result) => unwrap(result)),
        [1],
      ),
    ),
  component: lazyRouteComponent(() => import('./pages/community')),
})
const profile = createRoute({
  getParentRoute: () => authenticated,
  path: '/profile',
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      resourceOptions('passkeys', (signal) =>
        api.GET('/api/passkey', { signal }).then((result) => unwrap(result)),
      ),
    ),
  component: lazyRouteComponent(() => import('./pages/profile')),
})
const admin = createRoute({
  getParentRoute: () => authenticated,
  id: '_admin',
  beforeLoad: ({ context }) => {
    if (context.user.role !== 'admin' && context.user.role !== 'root')
      throw new APIError('无权执行此操作', 403)
  },
})
const channels = createRoute({
  getParentRoute: () => admin,
  path: '/channels',
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      resourceOptions(
        'channels',
        (signal) =>
          api
            .GET('/api/catalog/channels', {
              signal,
              params: { query: { page: 1, page_size: 30, keyword: '' } },
            })
            .then((result) => unwrap(result)),
        [1, ''],
      ),
    ),
  component: lazyRouteComponent(() => import('./pages/channels')),
})
const users = createRoute({
  getParentRoute: () => admin,
  path: '/users',
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      resourceOptions(
        'users',
        (signal) =>
          api
            .GET('/api/user/', { signal, params: { query: { page_size: 50 } } })
            .then((result) => unwrap(result)),
        [''],
      ),
    ),
  component: lazyRouteComponent(() => import('./pages/users')),
})
const settings = createRoute({
  getParentRoute: () => admin,
  path: '/settings',
  loader: ({ context }) =>
    context.queryClient.ensureQueryData(
      resourceOptions('settings', (signal) =>
        api.GET('/api/settings', { signal }).then((result) => unwrap(result)),
      ),
    ),
  component: lazyRouteComponent(() => import('./pages/settings')),
})
const channelMarket = createRoute({
  getParentRoute: () => authenticated,
  path: '/channel-market',
  loader: async ({ context }) => {
    const [page, pool] = await Promise.all([
      import('./pages/market'),
      import('./features/channelmarket/route-pools'),
    ])
    return Promise.all([
      context.queryClient.ensureQueryData(page.marketOptions()),
      context.queryClient.ensureQueryData(page.noticeOptions()),
      context.queryClient.ensureQueryData(keysOptions()),
      context.queryClient.ensureQueryData(pool.poolOptions()),
    ])
  },
  component: lazyRouteComponent(() => import('./pages/market')),
})
const myChannels = createRoute({
  getParentRoute: () => authenticated,
  path: '/my-channels',
  loader: async ({ context }) => {
    const [page, reports] = await Promise.all([
      import('./pages/market-owner'),
      import('./features/channelmarket/reports'),
    ])
    return Promise.all([
      context.queryClient.ensureQueryData(page.myChannelsOptions()),
      context.queryClient.ensureQueryData(reports.incomeOptions()),
      context.queryClient.ensureQueryData(reports.ownerLogsOptions()),
      context.queryClient.ensureQueryData(reports.ownerUsageOptions()),
      context.queryClient.ensureQueryData(reports.securityOptions()),
    ])
  },
  component: lazyRouteComponent(() => import('./pages/market-owner')),
})
const marketAdmin = createRoute({
  getParentRoute: () => admin,
  path: '/market-admin',
  loader: async ({ context }) => {
    const [page, reports] = await Promise.all([
      import('./pages/market-admin'),
      import('./features/channelmarket/reports'),
    ])
    return Promise.all([
      context.queryClient.ensureQueryData(page.marketAdminOptions()),
      context.queryClient.ensureQueryData(reports.incomeOptions(true)),
      context.queryClient.ensureQueryData(reports.securityOptions(true)),
    ])
  },
  component: lazyRouteComponent(() => import('./pages/market-admin')),
})
const transfers = createRoute({
  getParentRoute: () => authenticated,
  path: '/transfers',
  loader: async ({ context }) =>
    context.queryClient.ensureQueryData((await import('./pages/transfers')).transfersOptions()),
  component: lazyRouteComponent(() => import('./pages/transfers')),
})
const invoices = createRoute({
  getParentRoute: () => authenticated,
  path: '/invoices',
  loader: async ({ context }) => {
    const page = await import('./pages/invoices')
    return Promise.all([
      context.queryClient.ensureQueryData(page.eligibleInvoicesOptions()),
      context.queryClient.ensureQueryData(page.invoicesOptions()),
    ])
  },
  component: lazyRouteComponent(() => import('./pages/invoices')),
})
const redemptions = createRoute({
  getParentRoute: () => admin,
  path: '/redemptions',
  loader: async ({ context }) =>
    context.queryClient.ensureQueryData((await import('./pages/redemptions')).redemptionsOptions()),
  component: lazyRouteComponent(() => import('./pages/redemptions')),
})
const subscriptions = createRoute({
  getParentRoute: () => admin,
  path: '/subscriptions',
  loader: async ({ context }) =>
    context.queryClient.ensureQueryData(
      (await import('./pages/subscriptions')).adminPlansOptions(),
    ),
  component: lazyRouteComponent(() => import('./pages/subscriptions')),
})
const routeTree = root.addChildren([
  index,
  signIn,
  signUp,
  oauthCallback,
  authenticated.addChildren([
    dashboard,
    keys,
    wallet,
    logs,
    orders,
    groupBuy,
    blindBox,
    community,
    profile,
    channelMarket,
    myChannels,
    transfers,
    invoices,
    admin.addChildren([channels, users, settings, marketAdmin, redemptions, subscriptions]),
  ]),
])
export const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPendingComponent: Loading,
  defaultPendingMs: 150,
  defaultPreload: 'intent',
})
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
