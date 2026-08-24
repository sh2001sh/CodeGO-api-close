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
import { useMemo } from 'react'
import { useNavigate } from '@tanstack/react-router'
import {
  User,
  Wallet,
  LogOut,
  Settings,
  Sparkles,
  Workflow,
  ReceiptText,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { getUserAvatarFallback, getUserAvatarStyle } from '@/lib/avatar'
import { ROLE } from '@/lib/roles'
import useDialogState from '@/hooks/use-dialog'
import { useUserDisplay } from '@/hooks/use-user-display'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { SignOutDialog } from '@/components/sign-out-dialog'
import { useDailyLuckyNumberSelf } from '@/features/daily-lucky-number/hooks/use-daily-lucky-number'
import { getMembershipTierRank } from '@/features/daily-lucky-number/lib'
import { TierBadge } from '@/features/daily-lucky-number/components/tier-badge'

const avatarFallbackClassName = 'font-semibold text-white'

export function ProfileDropdown() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [open, setOpen] = useDialogState()
  const user = useAuthStore((state) => state.auth.user)
  const { displayName, roleLabel } = useUserDisplay(user)
  const isSuperAdmin = user?.role === ROLE.SUPER_ADMIN
  const avatarName = user?.username || displayName
  const avatarFallback = getUserAvatarFallback(avatarName)
  const avatarFallbackStyle = useMemo(
    () => getUserAvatarStyle(avatarName),
    [avatarName]
  )
  const dailyLuckyQuery = useDailyLuckyNumberSelf(Boolean(user))
  const featuredLuckyCard = useMemo(() => {
    const cards = dailyLuckyQuery.data?.subscriptions ?? []
    return [...cards].sort((left, right) => {
      const tierDifference =
        getMembershipTierRank(right.plan.membership_tier) -
        getMembershipTierRank(left.plan.membership_tier)
      if (tierDifference !== 0) return tierDifference
      return right.subscription.end_time - left.subscription.end_time
    })[0]
  }, [dailyLuckyQuery.data?.subscriptions])
  return (
    <>
      <DropdownMenu modal={false}>
        <DropdownMenuTrigger
          render={
            <Button
              variant='ghost'
              className='relative h-8 gap-1 rounded-full px-1.5'
            />
          }
        >
          <Avatar className='size-6'>
            <AvatarFallback
              className={`${avatarFallbackClassName} text-[11px]`}
              style={avatarFallbackStyle}
            >
              {avatarFallback}
            </AvatarFallback>
          </Avatar>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end' sideOffset={8} className='w-56'>
          <div className='flex items-center gap-2 px-1.5 py-1.5'>
            <Avatar className='size-8'>
              <AvatarFallback
                className={`${avatarFallbackClassName} text-xs`}
                style={avatarFallbackStyle}
              >
                {avatarFallback}
              </AvatarFallback>
            </Avatar>
            <div className='flex flex-1 flex-col gap-0.5 overflow-hidden'>
              <p className='text-foreground truncate text-sm font-medium'>
                {displayName}
              </p>
              <div className='flex items-center gap-1.5'>
                <span className='text-muted-foreground text-xs'>
                  {roleLabel}
                </span>
                {user?.group && (
                  <>
                    <span className='text-muted-foreground text-xs'>·</span>
                    <span className='text-muted-foreground truncate text-xs'>
                      {String(user.group)}
                    </span>
                  </>
                )}
              </div>
            </div>
          </div>

          <DropdownMenuSeparator />

          <DropdownMenuItem onClick={() => navigate({ to: '/profile' })}>
            <User className='size-4' />
            {t('Profile')}
          </DropdownMenuItem>

          <DropdownMenuItem onClick={() => navigate({ to: '/wallet' })}>
            <Wallet className='size-4' />
            {t('Wallet')}
          </DropdownMenuItem>

          <DropdownMenuItem onClick={() => navigate({ to: '/invoices' })}>
            <ReceiptText className='size-4' />
            电子发票
          </DropdownMenuItem>

          {featuredLuckyCard?.number ? (
            <DropdownMenuItem
              className='items-start gap-2'
              onClick={() => navigate({ to: '/daily-lucky-number' })}
            >
              <Sparkles className='mt-0.5 size-4 shrink-0' />
              <span className='min-w-0'>
                <span className='block text-xs font-medium'>
                  {t('Daily Lucky Number')}
                </span>
                <span className='mt-1 flex min-w-0 items-center gap-1.5'>
                  <TierBadge
                    tier={featuredLuckyCard.plan.membership_tier}
                    compact
                  />
                  <span className='truncate font-mono text-[11px] tabular-nums'>
                    {featuredLuckyCard.number.card_code}
                  </span>
                </span>
              </span>
            </DropdownMenuItem>
          ) : null}

          {isSuperAdmin && (
            <>
              <DropdownMenuItem
                onClick={() =>
                  navigate({ to: '/channels' })
                }
              >
                <Workflow className='size-4' />
                自动路由
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() =>
                  navigate({
                    to: '/system-settings/site/$section',
                    params: { section: 'system-info' },
                  })
                }
              >
                <Settings className='size-4' />
                {t('System Settings')}
              </DropdownMenuItem>
            </>
          )}

          <DropdownMenuSeparator />

          <DropdownMenuItem variant='destructive' onClick={() => setOpen(true)}>
            <LogOut className='size-4' />
            {t('Sign out')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <SignOutDialog open={!!open} onOpenChange={setOpen} />
    </>
  )
}
