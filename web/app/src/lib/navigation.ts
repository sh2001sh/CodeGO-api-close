import {
  Activity,
  Bell,
  BookOpen,
  Boxes,
  Cable,
  FileText,
  Gift,
  KeyRound,
  LayoutDashboard,
  ListOrdered,
  MessagesSquare,
  type LucideIcon,
  PackageOpen,
  RefreshCw,
  Rocket,
  Server,
  Settings,
  ShieldCheck,
  Star,
  Store,
  Ticket,
  UserCircle,
  UserPlus,
  Users,
  Wallet,
  Wrench,
  ArrowLeftRight,
  ScrollText,
  SearchCheck,
  Package,
  ClipboardCheck,
  Database,
  HeartPulse,
} from 'lucide-react'

export type Role = 'user' | 'admin' | 'root'

export type NavItem = {
  to: string
  label: string
  icon: LucideIcon
  /** Extra search terms (Chinese, English, legacy names) for the command palette. */
  keywords?: string
  /** Additional path prefixes that should mark this item active. */
  match?: readonly string[]
}

export type NavGroup = {
  id: string
  title: string
  minRole?: Role
  items: readonly NavItem[]
}

/**
 * Console information architecture. Groups follow the user's task order
 * (build → pay → discover → supply → govern). Active pages appear here
 * when it is an active destination. Legacy redirects stay outside navigation.
 */
export const consoleNav: readonly NavGroup[] = [
  {
    id: 'overview',
    title: '概览',
    items: [
      {
        to: '/dashboard',
        label: '仪表板',
        icon: LayoutDashboard,
        keywords: 'dashboard overview home 首页 总览',
      },
      {
        to: '/notifications',
        label: '消息通知',
        icon: Bell,
        keywords: 'notifications inbox unread 消息 通知 未读',
      },
    ],
  },
  {
    id: 'build',
    title: '开发',
    items: [
      { to: '/keys', label: 'API Key', icon: KeyRound, keywords: 'api key token 令牌 密钥' },
      {
        to: '/usage-logs',
        label: '使用日志',
        icon: Activity,
        keywords: 'usage logs activity 调用记录 请求日志 消费',
      },
      {
        to: '/playground',
        label: '对话',
        icon: MessagesSquare,
        keywords: 'playground chat test 调试 对话 测试',
      },
      {
        to: '/audit',
        label: '请求审计',
        icon: SearchCheck,
        keywords: 'audit requests attempts samples 审计 请求 重试 样本',
      },
    ],
  },
  {
    id: 'billing',
    title: '账单',
    items: [
      {
        to: '/wallet',
        label: '钱包',
        icon: Wallet,
        keywords: 'wallet balance top up recharge 充值 余额 额度 套餐 订阅 兑换',
      },
      { to: '/orders', label: '订单', icon: ListOrdered, keywords: 'orders 订单 支付记录 退款' },
      {
        to: '/billing',
        label: '账单明细',
        icon: ScrollText,
        keywords: 'billing ledger entries history invoice 账单 流水 明细 对账 发票',
      },
      {
        to: '/packages',
        label: '套餐包',
        icon: Package,
        keywords: 'packages 套餐包 购买 续费 升级',
      },
      {
        to: '/transfers',
        label: '钱包转账',
        icon: ArrowLeftRight,
        keywords: 'transfer 转账 赠送',
      },
      {
        to: '/referral-rewards',
        label: '邀请奖励',
        icon: UserPlus,
        keywords: 'referral invite affiliate 邀请 返利 推广',
      },
    ],
  },
  {
    id: 'discover',
    title: '发现',
    items: [
      {
        to: '/channel-market',
        label: '渠道市场',
        icon: Store,
        keywords: 'market marketplace group 分组 市场 倍率',
      },
      {
        to: '/group-favorites',
        label: '分组收藏',
        icon: Star,
        keywords: 'groups favorites bookmarks 分组 收藏 渠道',
      },
      { to: '/blind-box', label: '盲盒', icon: Gift, keywords: 'blind box 盲盒 抽卡' },
    ],
  },
  {
    id: 'supply',
    title: '供应',
    items: [
      {
        to: '/my-channels',
        label: '渠道工作台',
        icon: Cable,
        keywords: 'my channels owner 渠道主 上架 收益 结算',
      },
    ],
  },
  {
    id: 'admin',
    title: '管理',
    minRole: 'admin',
    items: [
      {
        to: '/channels',
        label: '渠道',
        icon: Server,
        keywords: 'channels upstream 渠道 上游 模型 分组 定价',
      },
      { to: '/users', label: '用户', icon: Users, keywords: 'users 用户 账号 封禁 余额调整' },
      {
        to: '/admin/logs',
        label: '全站日志',
        icon: Activity,
        keywords: 'admin logs all users 全站 日志 统计',
      },
      {
        to: '/admin/models',
        label: '模型目录',
        icon: Database,
        keywords: 'models catalog vendors prefill 模型 厂商 预填分组 元数据',
      },
      {
        to: '/admin/orders',
        label: '订单审核',
        icon: ClipboardCheck,
        keywords: 'orders review package payment 订单 审核 人工',
      },
      {
        to: '/market-admin',
        label: '市场审核',
        icon: ShieldCheck,
        keywords: 'market review 审核 渠道市场 安全审计',
      },
      {
        to: '/subscriptions',
        label: '套餐管理',
        icon: PackageOpen,
        keywords: 'plans subscription 套餐 订阅 月卡',
      },
      {
        to: '/redemptions',
        label: '兑换码管理',
        icon: Ticket,
        keywords: 'redemption codes 兑换码 卡密',
      },
      {
        to: '/admin/blind-box',
        label: '盲盒管理',
        icon: Gift,
        keywords: 'blind box admin pools 盲盒 奖池 发放 撤销',
      },
      { to: '/deployments', label: '模型部署', icon: Rocket, keywords: 'deployment 部署 模型' },
      {
        to: '/settings',
        label: '系统设置',
        icon: Settings,
        keywords: 'settings system 系统 设置 支付 登录 OAuth 公告',
      },
    ],
  },
  {
    id: 'ops',
    title: '运维',
    minRole: 'root',
    items: [
      {
        to: '/ratio-sync',
        label: '倍率同步',
        icon: RefreshCw,
        keywords: 'ratio sync 倍率 同步 价格',
      },
      {
        to: '/performance',
        label: '运行维护',
        icon: Wrench,
        keywords: 'performance maintenance 性能 维护 缓存',
      },
    ],
  },
  {
    id: 'account',
    title: '账户',
    items: [
      {
        to: '/profile',
        label: '个人资料',
        icon: UserCircle,
        keywords: 'profile account security 2fa passkey 密码 通行密钥 两步验证 绑定',
      },
    ],
  },
]

/** Public site links shown in the top bar and command palette. */
export const publicNav: readonly NavItem[] = [
  { to: '/models', label: '模型', icon: Boxes, keywords: 'models pricing 模型 价格 定价' },
  { to: '/channel-market', label: '渠道市场', icon: Store, keywords: 'market 市场 渠道' },
  { to: '/docs', label: '文档', icon: FileText, keywords: 'docs api 文档 接入' },
]

/** Supporting public pages also appear in the mobile drawer and command palette. */
export const resourceNav: readonly NavItem[] = [
  { to: '/help', label: '常见问题', icon: BookOpen, keywords: 'help faq 帮助 套餐 计费' },
  { to: '/status', label: '状态', icon: HeartPulse, keywords: 'status uptime 状态 可用率' },
  { to: '/support', label: '联系支持', icon: MessagesSquare, keywords: 'support 联系 客服 退款' },
  { to: '/about', label: '关于 CodeGo', icon: UserCircle, keywords: 'about company 公司 香港' },
]

const rank: Record<Role, number> = { user: 0, admin: 1, root: 2 }

export function visibleGroups(role: string | undefined): NavGroup[] {
  const current = rank[(role as Role) ?? 'user'] ?? 0
  return consoleNav.filter((group) => current >= rank[group.minRole ?? 'user'])
}

export function isActive(item: NavItem, pathname: string): boolean {
  const prefixes = [item.to, ...(item.match ?? [])]
  return prefixes.some((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`))
}
