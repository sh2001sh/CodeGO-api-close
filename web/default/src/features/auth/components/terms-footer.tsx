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
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { SystemStatus } from '../types'

interface TermsFooterProps {
  variant?: 'sign-in' | 'sign-up'
  className?: string
  status?: SystemStatus | null
}

export function TermsFooter(props: TermsFooterProps) {
  const { t } = useTranslation()
  if (
    !props.status?.user_agreement_enabled &&
    !props.status?.privacy_policy_enabled
  )
    return null
  return (
    <nav
      className={cn(
        'text-muted-foreground flex flex-wrap justify-center gap-x-6 gap-y-3 text-base',
        props.className
      )}
    >
      {props.status.user_agreement_enabled && (
        <a
          href='/user-agreement'
          className='hover:text-primary underline underline-offset-4'
        >
          {t('User Agreement')}
        </a>
      )}
      {props.status.privacy_policy_enabled && (
        <a
          href='/privacy-policy'
          className='hover:text-primary underline underline-offset-4'
        >
          {t('Privacy Policy')}
        </a>
      )}
    </nav>
  )
}
