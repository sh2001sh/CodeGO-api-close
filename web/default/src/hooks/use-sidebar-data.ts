import {
  Activity,
  BadgeCheck,
  Box,
  Command,
  Egg,
  FileText,
  Images,
  Gem,
  MessageSquare,
  Package,
  Radio,
  ShieldCheck,
  Store,
  RefreshCcw,
  ScrollText,
  Settings,
  Sparkles,
  Ticket,
  User,
  Users,
  LibraryBig,
  ReceiptText,
  ChartNoAxesCombined,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { WORKSPACE_IDS } from '@/components/layout/lib/workspace-registry'
import { type SidebarData } from '@/components/layout/types'

export function useSidebarData(): SidebarData {
  const { t } = useTranslation()

  return {
    workspaces: [
      {
        id: WORKSPACE_IDS.DEFAULT,
        name: '',
        logo: Command,
        plan: '',
      },
    ],
    navGroups: [
      {
        id: 'chat',
        title: t('Chat'),
        items: [
          {
            title: t('AI chat'),
            url: '/playground',
            icon: MessageSquare,
          },
          {
            title: t('Image workspace'),
            url: '/images',
            icon: Images,
          },
          {
            title: t('Presets'),
            icon: FileText,
            type: 'chat-presets',
          },
        ],
      },
      {
        id: 'general',
        title: t('General'),
        items: [
          {
            title: t('Overview'),
            url: '/dashboard/overview',
            icon: Activity,
          },
          {
            title: t('分组状态'),
            url: '/group-status',
            icon: ChartNoAxesCombined,
          },
          {
            title: t('分组市场'),
            url: '/marketplace',
            icon: Store,
          },
          {
            title: t('Model analytics'),
            url: '/dashboard/models',
            icon: Activity,
          },
          {
            title: t('API keys'),
            url: '/keys',
            icon: BadgeCheck,
          },
          {
            title: t('Usage logs'),
            url: '/usage-logs/common',
            icon: FileText,
          },
        ],
      },
      {
        id: 'personal',
        title: t('Personal'),
        items: [
          {
            title: t('Wallet'),
            url: '/wallet',
            icon: Gem,
          },
          {
            title: '电子发票',
            url: '/invoices',
            icon: ReceiptText,
          },
          {
            title: t('Blind box'),
            url: '/blind-box',
            icon: Ticket,
          },
          {
            title: t('Plans'),
            url: '/packages',
            icon: Package,
          },
          {
            title: t('Collective benefit plan'),
            url: '/group-buy',
            icon: Users,
          },
          {
            title: t('Daily Lucky Number'),
            url: '/daily-lucky-number',
            icon: Sparkles,
          },
          {
            title: t('Invites'),
            url: '/invite-rewards',
            icon: RefreshCcw,
          },
          {
            title: t('Profile'),
            url: '/profile',
            icon: User,
          },
          {
            title: t('Community resources'),
            url: '/community-resources',
            icon: LibraryBig,
          },
        ],
      },
      {
        id: 'admin',
        title: t('Admin'),
        items: [
          {
            title: t('Channels'),
            url: '/channels',
            icon: Radio,
          },
          {
            title: t('Models'),
            url: '/models/metadata',
            icon: Box,
          },
          {
            title: t('Users'),
            url: '/users',
            icon: Users,
          },
          {
            title: t('Redemption codes'),
            url: '/redemption-codes',
            icon: Ticket,
          },
          {
            title: t('Subscriptions'),
            url: '/subscriptions',
            icon: ScrollText,
          },
          {
            title: t('Blind box admin'),
            url: '/subscriptions#blind-box-admin',
            activeUrls: ['/subscriptions'],
            configUrls: ['/blind-box-admin'],
            icon: Egg,
          },
          {
            title: t('Daily Lucky Number'),
            url: '/subscriptions#daily-lucky-admin',
            activeUrls: ['/subscriptions'],
            configUrls: ['/daily-lucky-admin'],
            icon: Sparkles,
          },
          {
            title: t('System settings'),
            url: '/system-settings/site',
            activeUrls: ['/system-settings'],
            icon: Settings,
          },
          {
            title: t('Operations'),
            url: '/operations',
            icon: ShieldCheck,
          },
        ],
      },
    ],
  }
}
