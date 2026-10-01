import type { Page } from '@playwright/test'

export const user = {
  id: 1,
  username: 'operator',
  display_name: 'CodeGo 管理员',
  email: 'operator@example.test',
  role: 'admin',
  status: 'active',
  group: 'default',
  affiliate_micro_credits: 0,
}
const key = {
  id: 1,
  name: '工作密钥',
  key_prefix: 'sk-test',
  status: 'active',
  allowed_models: [],
  allowed_cidrs: [],
  group: null,
  expires_at: null,
  created_at: '2026-09-30T08:00:00Z',
  last_used_at: null,
}
export const plan = {
  id: 1,
  name: '开发者月卡',
  price_minor: 1000,
  currency: 'usd',
  credits: 100000000,
  period_seconds: 2592000,
  enabled: true,
  group_buy_enabled: true,
  group_buy_target: 3,
  group_buy_bonus: 10000000,
  group_buy_lifetime_seconds: 86400,
  period_credits: 0,
  reset_period: 'never',
  reset_custom_seconds: 0,
  internal_only: false,
  max_purchase_per_user: 0,
  duration_unit: 'month',
  duration_value: 1,
  custom_seconds: 0,
  group_buy_bonus2_micro: 0,
  group_buy_bonus3_micro: 0,
  group_buy_bonus5_micro: 0,
  plan_type: 'monthly',
  fuel_enabled: false,
  fuel_unit_price_micro: 0,
  fuel_min_credits: 0,
  fuel_credit_step: 0,
  upgrade_group: '',
  model_limits: {},
}
const channel = {
  id: 1,
  name: 'OpenAI 官方',
  provider: 'openai',
  base_url: 'https://api.openai.com',
  proxy_url: '',
  status: 'enabled',
  scope: 'official',
  owner_user_id: null,
  priority: 1,
  weight: 1,
  max_concurrency: 20,
  max_user_concurrency: 5,
  auto_disable: true,
  multiplier_card_supported: true,
  groups: ['default'],
  models: ['gpt-4o'],
  remark: '',
  settings: {},
  model_mapping: {},
  param_override: {},
  header_override: {},
  status_code_mapping: {},
}

export async function fixtureAPI(page: Page) {
  await page.route('**/api/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    let data: unknown = []
    if (path === '/api/user/self') data = user
    else if (path === '/api/user/') data = [user]
    else if (path === '/api/user/login' || path === '/api/user/register') data = { user }
    else if (path === '/api/token/' && route.request().method() === 'POST')
      data = { ...key, key: 'sk-created-test-only' }
    else if (path === '/api/token/') data = [key]
    else if (path === '/api/wallet')
      data = {
        account_id: 1,
        balance_micro_credits: '123456789',
        balance_micro: 123456789,
        version: 0,
      }
    else if (path === '/api/subscription/plans') data = [plan]
    else if (path === '/api/subscription/admin/plans') data = [plan]
    else if (path === '/api/commerce/providers')
      data = [{ provider: 'stripe', currency: 'usd', credits_per_minor: 100000 }]
    else if (path === '/api/subscription/self')
      data = [
        {
          id: 1,
          plan_id: 1,
          state: 'active',
          balance: 90000000,
          expires_at: '2026-10-30T08:00:00Z',
        },
      ]
    else if (path === '/api/subscription/self/preference')
      data = {
        billing_preference: 'subscription_first',
        funding_source_order: ['subscription', 'wallet'],
        subscription_order_ids: [],
      }
    else if (path === '/api/wallet/refunds/eligible') data = { items: [] }
    else if (path === '/api/commerce/orders' || path === '/api/commerce/admin/orders')
      data = [
        {
          id: 1,
          trade_no: 'v3_example',
          kind: 'subscription',
          plan_id: 1,
          state: 'paid',
          amount_minor: 1000,
          currency: 'usd',
          credits: 100000000,
          group_buy_enabled: true,
          created_at: '2026-09-30T08:00:00Z',
        },
      ]
    else if (path === '/api/log/self')
      data = {
        items: [
          {
            request_id: 'fixture-request',
            model: 'gpt-4o',
            amount: 1234,
            prompt_tokens: 100,
            completion_tokens: 20,
            terminal: 'completed',
            created_at: '2026-09-30T08:00:00Z',
          },
        ],
      }
    else if (path === '/api/group-buy/list' || path === '/api/group-buy/mine')
      data = [
        {
          id: 1,
          plan_id: 1,
          current_count: 1,
          target_count: 3,
          bonus_micro: 10000000,
          status: 'pending',
          expires_at: '2026-10-02T08:00:00Z',
        },
      ]
    else if (path === '/api/blind-box/self')
      data = {
        available_count: 2,
        pools: [
          {
            id: 1,
            name: '日常盲盒',
            enabled: true,
            price_micro: 1000000,
            daily_limit: 5,
            rewards: [{ kind: 'credits', title: '1 credit', amount_micro: 1000000, weight: 1 }],
          },
        ],
        props: [],
      }
    else if (path === '/api/catalog/channels') data = { items: [channel], total: 1 }
    else if (path === '/api/settings')
      data = [{ key: 'site_name', value: 'CodeGo', sensitive: false, configured: true }]
    else if (path === '/api/community/sellers' || path === '/api/community/v1/sellers')
      data = {
        items: [
          {
            sub: 'codego:2',
            username: 'seller',
            display_name: '模型渠道主',
            channel_count: 1,
            average_score: 9,
            rating_count: 2,
            channels: [
              {
                id: '2',
                name: '公共渠道',
                provider: 'openai',
                average_score: 9,
                rating_count: 2,
                viewer_stars: 0,
              },
            ],
          },
        ],
        total: 1,
      }
    else if (path === '/api/passkey') data = { enabled: false, count: 0 }
    else if (path === '/api/oauth/providers') data = [{ slug: 'github', name: 'GitHub', icon: '' }]
    else if (path === '/api/wallet/transfers')
      data = {
        micro_per_credit: 1000000,
        min_micro: 1000000,
        balance: 100000000,
        fee_bps: 100,
        security: {
          password_set: true,
          locked_until: 0,
          remaining_password_attempts: 5,
          requires_account_password: true,
          email_bound: true,
          email_masked: 'o***@example.test',
          email_recovery_available: false,
        },
        history: { page: 1, page_size: 10, total: 0, items: [] },
      }
    else if (path === '/api/invoices/requests' || path === '/api/invoices/admin/requests')
      data = { page: 1, page_size: 20, total: 0, items: [] }
    else if (
      path === '/api/marketplace/key-group-options' ||
      path === '/api/marketplace/groups' ||
      path === '/api/marketplace/channels/mine' ||
      path === '/api/marketplace/admin/channels'
    )
      data = [
        {
          id: 'public-channel',
          internal_channel_id: 1,
          owner_user_id: 1,
          group_id: 'group-test',
          public_slug: 'public-test',
          system_display_name: '公共模型渠道',
          provider_type: 'openai',
          approved_source_label: '公共渠道',
          declared_models: ['gpt-4o'],
          model_prices: {},
          multiplier_ppm: 1000000,
          multiplier: 1,
          visibility: 'public',
          lifecycle_status: 'active',
          verification_status: 'verified',
          last_review_reason: '',
          created_at: '2026-09-30T08:00:00Z',
          updated_at: '2026-09-30T08:00:00Z',
        },
      ]
    else if (path === '/api/marketplace/auto-route-pool')
      data = { enabled: false, name: '自动路由', members: [] }
    else if (path === '/api/marketplace/channels/mine/user-usage') data = {}
    else if (path.endsWith('/security-audit/events'))
      data = { items: [], total: 0, page: 1, page_size: 20 }
    await route.fulfill({ json: { success: true, data } })
  })
}
