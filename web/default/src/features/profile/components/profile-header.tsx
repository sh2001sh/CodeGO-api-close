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
import { Activity, BarChart3, Hash, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatCompactNumber, formatQuota } from '@/lib/format'
import { getRoleLabel } from '@/lib/roles'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusBadge } from '@/components/status-badge'
import { getUserInitials, getDisplayName } from '../lib'
import type { UserProfile } from '../types'

// ============================================================================
// Profile Header Component
// ============================================================================

interface ProfileHeaderProps {
  profile: UserProfile | null
  loading: boolean
}

export function ProfileHeader({ profile, loading }: ProfileHeaderProps) {
  const { t } = useTranslation()

  if (loading) {
    return (
      <section className='profile-identity'>
        <div className='profile-identity-header'>
          <div className='flex flex-col items-center gap-4 text-center sm:flex-row sm:text-left'>
            <Skeleton className='h-16 w-16 rounded-2xl' />
            <div className='space-y-3'>
              <div className='flex flex-col items-center gap-2 sm:flex-row sm:justify-start'>
                <Skeleton className='h-8 w-48' />
                <Skeleton className='h-5 w-16' />
              </div>
              <div className='flex flex-col items-center gap-1 sm:flex-row sm:justify-start sm:gap-4'>
                <Skeleton className='h-4 w-24' />
                <Skeleton className='h-4 w-40' />
                <Skeleton className='h-4 w-20' />
              </div>
            </div>
          </div>
        </div>
        <div className='border-t'>
          <div className='profile-identity-stats'>
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className='profile-identity-stat'>
                <Skeleton className='h-3.5 w-20' />
                <Skeleton className='mt-2 h-7 w-28' />
                <Skeleton className='mt-1.5 h-3.5 w-24' />
              </div>
            ))}
          </div>
        </div>
      </section>
    )
  }

  if (!profile) return null

  const displayName = getDisplayName(profile)
  const initials = getUserInitials(profile)
  const roleLabel = getRoleLabel(profile.role)
  const stats = [
    {
      label: t('Universal Credit'),
      value: formatQuota(profile.quota),
      icon: WalletCards,
    },
    {
      label: t('Total Usage'),
      value: formatQuota(profile.used_quota),
      icon: BarChart3,
    },
    {
      label: t('API Requests'),
      value: formatCompactNumber(profile.request_count),
      icon: Activity,
    },
  ]

  return (
    <section className='profile-identity'>
      <header className='profile-identity-header'>
        <div className='profile-identity-person'>
          <Avatar className='ring-background h-12 w-12 rounded-xl text-sm ring-2 sm:h-16 sm:w-16 sm:rounded-2xl sm:text-lg sm:ring-4'>
            <AvatarFallback className='bg-primary/10 text-primary rounded-xl sm:rounded-2xl'>
              {initials}
            </AvatarFallback>
          </Avatar>

          <div className='profile-identity-details'>
            <div className='flex min-w-0 items-center gap-2'>
              <h2 className='text-2xl font-medium tracking-tight break-words'>
                {displayName}
              </h2>
              <StatusBadge
                label={roleLabel}
                variant='neutral'
                copyable={false}
              />
            </div>

            <div className='profile-identity-meta'>
              <span className='truncate'>@{profile.username}</span>
              {profile.external_id && (
                <>
                  <span>•</span>
                  <StatusBadge
                    label={`ID ${profile.external_id}`}
                    copyText={profile.external_id}
                    icon={Hash}
                    showDot={false}
                    variant='neutral'
                    className='font-mono tabular-nums'
                  />
                </>
              )}
              {profile.email && (
                <>
                  <span>•</span>
                  <span className='truncate'>{profile.email}</span>
                </>
              )}
              {profile.group && (
                <>
                  <span>•</span>
                  <span className='truncate'>{profile.group}</span>
                </>
              )}
            </div>
          </div>
        </div>
      </header>
      <dl className='profile-identity-stats'>
        {stats.map((item) => (
          <div key={item.label} className='profile-identity-stat'>
            <dt className='flex items-center gap-2'>
              <item.icon className='text-muted-foreground/60 size-3.5 shrink-0' />
              <span className='profile-stat-label'>{item.label}</span>
            </dt>

            <dd className='profile-stat-value'>{item.value}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}
