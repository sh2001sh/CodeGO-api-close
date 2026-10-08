// Settings field definitions: site, auth and billing/payment groups.
// Split out of registry.ts to stay under the 400-line file limit.
// Every key is grounded in a confirmed live (non-test) read in the v3 Go backend.
import type { SettingFieldDef } from './registry-types'

export const coreFields: readonly SettingFieldDef[] = [
  // 站点 — v3/internal/identity/control_oauth_builtin.go:19
  {
    key: 'ServerAddress',
    label: '站点公开地址',
    description: 'OAuth 回调等场景使用的站点公开 URL',
    type: 'string',
    group: 'site',
    placeholder: 'https://example.com',
  },
  // 站点 — v3/internal/desktop/status.go:11
  {
    key: 'Notice',
    label: '系统公告',
    description: '展示给前端的服务公告文本',
    type: 'string',
    group: 'site',
  },
  {
    key: 'Maintenance',
    label: '维护模式',
    description: '开启后服务状态标记为维护中',
    type: 'boolean',
    group: 'site',
  },

  // 登录与认证 — v3/internal/identity/control_email_policy.go:12
  {
    key: 'EmailVerificationEnabled',
    label: '注册邮箱验证码',
    description: '注册时要求邮箱验证码',
    type: 'boolean',
    group: 'auth',
    section: '邮箱策略',
  },
  {
    key: 'EmailDomainRestrictionEnabled',
    label: '限制邮箱域名',
    description: '仅允许白名单域名的邮箱注册',
    type: 'boolean',
    group: 'auth',
    section: '邮箱策略',
  },
  {
    key: 'EmailDomainWhitelist',
    label: '邮箱域名白名单',
    description: '仅在限制开启时生效',
    type: 'json',
    group: 'auth',
    section: '邮箱策略',
    hint: '可填 JSON 数组，如 ["example.com"]',
  },
  {
    key: 'EmailAliasRestrictionEnabled',
    label: '限制邮箱别名',
    description: '禁止使用 + 别名等方式重复注册',
    type: 'boolean',
    group: 'auth',
    section: '邮箱策略',
  },

  // 登录与认证 / OAuth — v3/internal/identity/control_oauth_builtin.go:14-20
  {
    key: 'GitHubOAuthEnabled',
    label: '启用 GitHub 登录',
    type: 'boolean',
    group: 'auth',
    section: 'GitHub',
  },
  {
    key: 'GitHubClientId',
    label: 'GitHub Client ID',
    type: 'string',
    group: 'auth',
    section: 'GitHub',
  },
  {
    key: 'GitHubClientSecret',
    label: 'GitHub Client Secret',
    type: 'secret',
    group: 'auth',
    section: 'GitHub',
  },
  {
    key: 'LinuxDOOAuthEnabled',
    label: '启用 LinuxDO 登录',
    type: 'boolean',
    group: 'auth',
    section: 'LinuxDO',
  },
  {
    key: 'LinuxDOClientId',
    label: 'LinuxDO Client ID',
    type: 'string',
    group: 'auth',
    section: 'LinuxDO',
  },
  {
    key: 'LinuxDOClientSecret',
    label: 'LinuxDO Client Secret',
    type: 'secret',
    group: 'auth',
    section: 'LinuxDO',
  },
  {
    key: 'LinuxDOMinimumTrustLevel',
    label: 'LinuxDO 最低信任等级',
    type: 'number',
    group: 'auth',
    section: 'LinuxDO',
  },
  {
    key: 'discord.enabled',
    label: '启用 Discord 登录',
    type: 'boolean',
    group: 'auth',
    section: 'Discord',
  },
  {
    key: 'discord.client_id',
    label: 'Discord Client ID',
    type: 'string',
    group: 'auth',
    section: 'Discord',
  },
  {
    key: 'discord.client_secret',
    label: 'Discord Client Secret',
    type: 'secret',
    group: 'auth',
    section: 'Discord',
  },
  { key: 'oidc.enabled', label: '启用 OIDC 登录', type: 'boolean', group: 'auth', section: 'OIDC' },
  {
    key: 'oidc.client_id',
    label: 'OIDC Client ID',
    type: 'string',
    group: 'auth',
    section: 'OIDC',
  },
  {
    key: 'oidc.client_secret',
    label: 'OIDC Client Secret',
    type: 'secret',
    group: 'auth',
    section: 'OIDC',
  },
  {
    key: 'oidc.authorization_endpoint',
    label: 'OIDC 授权端点',
    type: 'string',
    group: 'auth',
    section: 'OIDC',
  },
  {
    key: 'oidc.token_endpoint',
    label: 'OIDC Token 端点',
    type: 'string',
    group: 'auth',
    section: 'OIDC',
  },
  {
    key: 'oidc.user_info_endpoint',
    label: 'OIDC 用户信息端点',
    type: 'string',
    group: 'auth',
    section: 'OIDC',
  },

  // 登录与认证 / 分组授权 — v3/migrations/20261001000070_identity_policies.sql:17-20,80
  {
    key: 'UserUsableGroups',
    label: '全局可用分组',
    description: '分组名到展示名的映射，决定普通用户默认可见的分组',
    type: 'json',
    group: 'auth',
    section: '分组授权',
    hint: '例如 {"default":"默认分组","vip":"vip分组"}',
  },
  {
    key: 'AutoGroups',
    label: '自动授权分组',
    description: '新用户自动获得访问权限的分组名数组',
    type: 'json',
    group: 'auth',
    section: '分组授权',
    hint: '例如 ["default"]',
  },
  {
    key: 'group_ratio_setting.group_special_usable_group',
    label: '分组专属可用分组',
    description: '按用户所属分组覆盖可见分组列表',
    type: 'json',
    group: 'auth',
    section: '分组授权',
  },

  // 计费与额度 / 订阅策略 — compile.go:74, catalogcontrol/settings.go:125; subscription_conversion_policy.go:28
  {
    key: 'SubscriptionGroupPolicy',
    label: '订阅分组策略',
    description: 'JSON 对象，键为分组名，值含 enabled / multiplier_ppm / paid_only',
    type: 'json',
    group: 'billing',
    section: '订阅策略',
    hint: '例如 {"default":{"enabled":true,"multiplier_ppm":1000000,"paid_only":false}}',
  },
  {
    key: 'SubscriptionClaudeConversionEnabled',
    label: '允许订阅余额转换',
    description: '是否允许将订阅额度转换为 Claude 消耗',
    type: 'boolean',
    group: 'billing',
    section: '订阅策略',
  },
  // 计费与额度 / 计费表达式 — v3/internal/legacy/options.go:187,192
  {
    key: 'billing_setting.billing_mode',
    label: '模型计费模式',
    description: '模型名到计费模式的映射，如 tiered_expr',
    type: 'json',
    group: 'billing',
    section: '计费表达式',
  },
  {
    key: 'billing_setting.billing_expr',
    label: '模型计费表达式',
    description: '模型名到计费表达式字符串的映射，详见 billingexpr 文档',
    type: 'json',
    group: 'billing',
    section: '计费表达式',
  },

  // 支付 — v3/internal/commerce/checkout_discount_campaign.go:21-23
  {
    key: 'InvoiceSellerAddress',
    label: '发票开具方地址',
    description: '用于商业发票的公司真实地址；未配置时不可开具新发票',
    type: 'string',
    group: 'payment',
    section: '商业发票',
  },
  {
    key: 'payment_setting.first_purchase_discount_enabled',
    label: '首购折扣开关',
    type: 'boolean',
    group: 'payment',
    section: '首购折扣',
  },
  {
    key: 'payment_setting.first_purchase_discount_multiplier',
    label: '首购折扣倍率',
    description: '小于 1 的小数，例如 0.8 代表八折',
    type: 'string',
    group: 'payment',
    section: '首购折扣',
    placeholder: '0.8',
  },
  {
    key: 'payment_setting.first_purchase_discount_start_at',
    label: '首购折扣开始时间（Unix 秒）',
    type: 'number',
    group: 'payment',
    section: '首购折扣',
  },
  {
    key: 'payment_setting.first_purchase_discount_end_at',
    label: '首购折扣结束时间（Unix 秒）',
    type: 'number',
    group: 'payment',
    section: '首购折扣',
  },

  // 模型与部署 — v3/internal/adminops/deployments_client.go:23,36
  {
    key: 'model_deployment.ionet.enabled',
    label: '启用 io.net 算力部署',
    type: 'boolean',
    group: 'model',
  },
  {
    key: 'model_deployment.ionet.api_key',
    label: 'io.net API Key',
    type: 'secret',
    group: 'model',
  },
]
