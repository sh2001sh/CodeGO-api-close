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
import {
  Box,
  Cable,
  Clover,
  Egg,
  FlaskConical,
  Gauge,
  Gift,
  KeyRound,
  Package,
  ReceiptText,
  ScrollText,
  Settings,
  ShieldCheck,
  Store,
  Ticket,
  User,
  UserPlus,
  Users,
  UsersRound,
  Wallet,
} from 'lucide-react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { WORKSPACE_IDS } from '@/components/layout/lib/workspace-registry'
import { type SidebarData } from '@/components/layout/types'

export function buildSidebarData(t: TFunction): SidebarData {
  return {
    workspaces: [
      {
        id: WORKSPACE_IDS.DEFAULT,
        name: '',
        logo: Gauge,
        plan: '',
      },
    ],
    navGroups: [
      {
        id: 'use',
        title: t('使用'),
        items: [
          {
            title: t('Overview'),
            url: '/dashboard/overview',
            icon: Gauge,
          },
          {
            title: t('API keys'),
            url: '/keys',
            icon: KeyRound,
          },
          {
            title: t('Usage logs'),
            url: '/usage-logs/common',
            icon: ScrollText,
          },
          {
            title: t('调试'),
            url: '/playground',
            icon: FlaskConical,
          },
        ],
      },
      {
        id: 'assets',
        title: t('资产'),
        items: [
          {
            title: t('Wallet'),
            url: '/wallet',
            icon: Wallet,
          },
          {
            title: t('Plans'),
            url: '/packages',
            icon: Package,
          },
          {
            title: t('Blind box'),
            url: '/blind-box',
            icon: Gift,
          },
        ],
      },
      {
        id: 'personal',
        title: t('个人'),
        items: [
          {
            title: t('Profile'),
            url: '/profile',
            icon: User,
          },
        ],
      },
      {
        id: 'more',
        title: t('更多'),
        items: [
          {
            title: t('更多'),
            icon: UserPlus,
            items: [
              {
                title: t('Daily Lucky Number'),
                url: '/daily-lucky-number',
                icon: Clover,
              },
              {
                title: t('Community resources'),
                url: '/community-resources',
                icon: Users,
              },
              {
                title: t('Invites'),
                url: '/invite-rewards',
                icon: UserPlus,
              },
              {
                title: t('Collective benefit plan'),
                url: '/group-buy',
                icon: UsersRound,
              },
              {
                title: '电子发票',
                url: '/invoices',
                icon: ReceiptText,
              },
            ],
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
            icon: Cable,
          },
          {
            title: t('分组市场'),
            url: '/marketplace',
            icon: Store,
          },
          {
            title: t('管理工具'),
            icon: Settings,
            items: [
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
                icon: Clover,
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
      },
    ],
  }
}

export function useSidebarData(): SidebarData {
  const { t } = useTranslation()
  return buildSidebarData(t)
}
