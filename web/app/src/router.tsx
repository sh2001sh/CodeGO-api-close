import { QueryClient } from '@tanstack/react-query'
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  defaultParseSearch,
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
import { PageMetadata } from './components/page-metadata'

type AuthSearch = { returnTo?: string; ref?: string }
const authSearch = (search: AuthSearch): AuthSearch => ({
  returnTo: safeLocalReturn(search.returnTo),
  ref:
    typeof search.ref === 'string' && /^[A-Za-z0-9_-]{1,100}$/.test(search.ref)
      ? search.ref
      : undefined,
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
  component: () => (
    <>
      <PageMetadata />
      <Outlet />
    </>
  ),
  errorComponent: RouteError,
  notFoundComponent: () => (
    <section className="page">
      <h1>404</h1>
    </section>
  ),
})
const site = createRoute({
  getParentRoute: () => root,
  id: '_site',
  component: lazyRouteComponent(() => import('./components/site-layout'), 'SiteLayout'),
})
const index = createRoute({
  getParentRoute: () => site,
  path: '/',
  component: lazyRouteComponent(() => import('./pages/home')),
})
type ModelsSearch = { q?: string; vendor?: string }
const models = createRoute({
  getParentRoute: () => site,
  path: '/models',
  validateSearch: (search: ModelsSearch): ModelsSearch => ({
    q: typeof search.q === 'string' && search.q ? search.q.slice(0, 100) : undefined,
    vendor:
      typeof search.vendor === 'string' && search.vendor ? search.vendor.slice(0, 40) : undefined,
  }),
  component: lazyRouteComponent(() => import('./pages/models')),
})
const docs = createRoute({
  getParentRoute: () => site,
  path: '/docs',
  validateSearch: (search: { article?: string }): { article?: string } => ({
    article:
      typeof search.article === 'string' && /^[a-z0-9-]{1,80}$/.test(search.article)
        ? search.article
        : undefined,
  }),
  component: lazyRouteComponent(() => import('./pages/docs')),
})
const publicResource = <TPath extends string>(
  path: TPath,
  exportName:
    | 'HelpPage'
    | 'SupportPage'
    | 'AboutPage'
    | 'PrivacyPage'
    | 'TermsPage'
    | 'RefundPolicyPage'
    | 'SupplierPolicyPage'
    | 'MarketRulesPage',
) =>
  createRoute({
    getParentRoute: () => site,
    path,
    component: lazyRouteComponent(() => import('./pages/resources'), exportName),
  })
const help = publicResource('/help', 'HelpPage')
const support = publicResource('/support', 'SupportPage')
const about = publicResource('/about', 'AboutPage')
const privacy = publicResource('/privacy', 'PrivacyPage')
const terms = publicResource('/terms', 'TermsPage')
const refundPolicy = publicResource('/refund-policy', 'RefundPolicyPage')
const supplierAgreement = publicResource('/supplier-agreement', 'SupplierPolicyPage')
const supplierTerms = createRoute({
  getParentRoute: () => site,
  path: '/supplier-terms',
  beforeLoad: () => {
    throw redirect({ to: '/supplier-agreement' })
  },
})
const marketRules = publicResource('/market-rules', 'MarketRulesPage')
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
const forgotPassword = createRoute({
  getParentRoute: () => root,
  path: '/forgot-password',
  component: lazyRouteComponent(() => import('./pages/password-recovery')),
})
const resetPassword = createRoute({
  getParentRoute: () => root,
  path: '/reset',
  validateSearch: (search: { email?: string; token?: string }) => ({
    email: search.email ?? '',
    token: search.token ?? '',
  }),
  component: lazyRouteComponent(() => import('./pages/password-recovery'), 'ResetPasswordPage'),
})
const legacyResetPassword = createRoute({
  getParentRoute: () => root,
  path: '/user/reset',
  validateSearch: (search: { email?: string; token?: string }) => ({
    email: search.email ?? '',
    token: search.token ?? '',
  }),
  component: lazyRouteComponent(() => import('./pages/password-recovery'), 'ResetPasswordPage'),
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
  validateSearch: (search: { group?: string }): { group?: string } => ({
    group:
      typeof search.group === 'string' && search.group ? search.group.slice(0, 255) : undefined,
  }),
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
  beforeLoad: () => {
    throw redirect({ to: '/dashboard' })
  },
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
  getParentRoute: () => root,
  path: '/community',
  beforeLoad: () => {
    throw redirect({ href: 'https://community.codegoai.com' })
  },
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
const desktopAuthorize = createRoute({
  getParentRoute: () => authenticated,
  path: '/desktop/authorize',
  beforeLoad: () => {
    throw redirect({ to: '/profile' })
  },
})
const desktopDevices = createRoute({
  getParentRoute: () => authenticated,
  path: '/desktop/devices',
  beforeLoad: () => {
    throw redirect({ to: '/profile' })
  },
})
const groupFavorites = createRoute({
  getParentRoute: () => authenticated,
  path: '/group-favorites',
  component: lazyRouteComponent(() => import('./pages/group-favorites')),
})
const retiredModelFavorites = createRoute({
  getParentRoute: () => authenticated,
  path: '/model-favorites',
  beforeLoad: () => {
    throw redirect({ to: '/group-favorites', replace: true })
  },
})
const referralRewards = createRoute({
  getParentRoute: () => authenticated,
  path: '/referral-rewards',
  component: lazyRouteComponent(() => import('./pages/referral-rewards')),
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
const deployments = createRoute({
  getParentRoute: () => admin,
  path: '/deployments',
  component: lazyRouteComponent(() => import('./pages/deployments')),
})
const rootAdmin = createRoute({
  getParentRoute: () => admin,
  id: '_root-admin',
  beforeLoad: ({ context }) => {
    if (context.user.role !== 'root') throw new APIError('无权执行此操作', 403)
  },
})
const ratioSync = createRoute({
  getParentRoute: () => rootAdmin,
  path: '/ratio-sync',
  component: lazyRouteComponent(() => import('./pages/ratio-sync')),
})
const performance = createRoute({
  getParentRoute: () => rootAdmin,
  path: '/performance',
  component: lazyRouteComponent(() => import('./pages/performance-tools')),
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
const marketFrame = createRoute({
  getParentRoute: () => root,
  id: '_market',
  component: lazyRouteComponent(() => import('./components/market-layout'), 'MarketLayout'),
})
type MarketSearch = { group?: string; model?: string; shop?: string; view?: 'groups' | 'shops' }
const channelMarket = createRoute({
  getParentRoute: () => marketFrame,
  path: '/channel-market',
  validateSearch: (search: MarketSearch): MarketSearch => ({
    view: search.view === 'groups' || search.view === 'shops' ? search.view : undefined,
    group:
      typeof search.group === 'string' && /^[A-Za-z0-9_-]{1,120}$/.test(search.group)
        ? search.group
        : undefined,
    model:
      typeof search.model === 'string' && search.model ? search.model.slice(0, 120) : undefined,
    shop:
      typeof search.shop === 'string' && /^[A-Za-z0-9_-]{1,120}$/.test(search.shop)
        ? search.shop
        : undefined,
  }),
  component: lazyRouteComponent(() => import('./pages/market')),
})
const notifications = createRoute({
  getParentRoute: () => authenticated,
  path: '/notifications',
  component: lazyRouteComponent(() => import('./pages/notifications')),
})
const myChannels = createRoute({
  getParentRoute: () => authenticated,
  path: '/my-channels',
  loader: async ({ context }) => {
    const page = await import('./pages/market-owner')
    return context.queryClient.ensureQueryData(page.myChannelsOptions())
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
  beforeLoad: () => {
    throw redirect({ to: '/billing', hash: 'invoices' })
  },
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
// Pages that own their data fetching (no route loader). Each file default-exports its page.
// Generic over parent and path so the literal path survives into the typed route tree.
const lazyPage = <
  TParent extends typeof site | typeof authenticated | typeof admin,
  TPath extends string,
>(
  parent: TParent,
  path: TPath,
  load: () => Promise<{ default: () => React.JSX.Element }>,
) => createRoute({ getParentRoute: () => parent, path, component: lazyRouteComponent(load) })
const status = lazyPage(site, '/status', () => import('./pages/status'))
const download = createRoute({
  getParentRoute: () => site,
  path: '/download',
  beforeLoad: () => {
    throw redirect({ to: '/docs' })
  },
})
const playground = createRoute({
  getParentRoute: () => authenticated,
  path: '/playground',
  validateSearch: (search: {
    group?: string
    model?: string
  }): { group?: string; model?: string } => ({
    group:
      typeof search.group === 'string' && search.group ? search.group.slice(0, 255) : undefined,
    model:
      typeof search.model === 'string' && search.model ? search.model.slice(0, 255) : undefined,
  }),
  component: lazyRouteComponent(() => import('./pages/playground')),
})
const billingHistory = lazyPage(authenticated, '/billing', () => import('./pages/billing-history'))
const audit = lazyPage(authenticated, '/audit', () => import('./pages/audit'))
const packages = lazyPage(authenticated, '/packages', () => import('./pages/packages'))
const adminLogs = lazyPage(admin, '/admin/logs', () => import('./pages/admin-logs'))
const modelCatalog = lazyPage(admin, '/admin/models', () => import('./pages/model-catalog'))
const adminOrders = lazyPage(admin, '/admin/orders', () => import('./pages/admin-orders'))
const blindBoxAdmin = lazyPage(admin, '/admin/blind-box', () => import('./pages/blind-box-admin'))

const routeTree = root.addChildren([
  site.addChildren([
    index,
    models,
    docs,
    status,
    download,
    help,
    support,
    about,
    privacy,
    terms,
    refundPolicy,
    supplierAgreement,
    supplierTerms,
    marketRules,
  ]),
  signIn,
  signUp,
  oauthCallback,
  forgotPassword,
  resetPassword,
  legacyResetPassword,
  community,
  marketFrame.addChildren([channelMarket]),
  authenticated.addChildren([
    dashboard,
    notifications,
    keys,
    wallet,
    logs,
    orders,
    groupBuy,
    blindBox,
    profile,
    desktopAuthorize,
    desktopDevices,
    groupFavorites,
    retiredModelFavorites,
    referralRewards,
    myChannels,
    transfers,
    invoices,
    playground,
    billingHistory,
    audit,
    packages,
    admin.addChildren([
      adminLogs,
      modelCatalog,
      adminOrders,
      blindBoxAdmin,
      channels,
      users,
      settings,
      marketAdmin,
      redemptions,
      subscriptions,
      deployments,
      rootAdmin.addChildren([ratioSync, performance]),
    ]),
  ]),
])
export const router = createRouter({
  routeTree,
  parseSearch: (search) => {
    const parsed = defaultParseSearch(search) as Record<string, unknown>
    // Marketplace IDs are identifiers, including decimal IDs above JS's safe integer range.
    for (const field of ['group', 'shop']) {
      const id = new URLSearchParams(search).get(field)
      if (id !== null && typeof parsed[field] !== 'string') parsed[field] = id
    }
    return parsed
  },
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
