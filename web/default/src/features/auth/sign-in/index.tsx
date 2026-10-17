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
import { Link, useSearch } from '@tanstack/react-router'
import i18n from '@/i18n/config'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { normalizeSystemName } from '@/lib/branding'
import { DEFAULT_LOGO } from '@/lib/constants'
import { useStatus } from '@/hooks/use-status'
import { SiteSeo } from '@/components/seo'
import { AuthLayout } from '../auth-layout'
import { TermsFooter } from '../components/terms-footer'
import { UserAuthForm } from './components/user-auth-form'

export function SignIn() {
  const { t } = useTranslation()
  const { redirect } = useSearch({ from: '/(auth)/sign-in' })
  const { status } = useStatus()
  const brand = normalizeSystemName()

  return (
    <AuthLayout>
      <SiteSeo
        title={i18n.t('登录')}
        description={i18n.t(
          '登录 Code Go 继续管理你的 API Key、模型、额度与工作流。'
        )}
        canonicalPath='/sign-in'
        robots='noindex,follow'
      />
      <div className='w-full'>
        {/* Top Brand Logo & Title (Vercel & OpenDesign style) */}
        <div className='market-auth-header'>
          <div className='market-auth-brand-emblem' aria-hidden='true'>
            <img
              src={DEFAULT_LOGO}
              alt=''
              width='32'
              height='32'
              className='size-8 object-contain'
            />
          </div>
          <h1 className='market-auth-title'>
            {t('Sign in to {{brand}}', { brand })}
          </h1>
        </div>

        <UserAuthForm redirectTo={redirect} />

        {/* Switch to Sign Up */}
        {!status?.self_use_mode_enabled && (
          <div className='market-auth-switch-row'>
            <span>{t("Don't have an account?")}</span>
            <Link
              to='/sign-up'
              search={redirect ? { redirect } : {}}
              className='market-auth-switch-link'
            >
              {t('Sign up')}
              <ArrowRight className='ml-1 inline-block size-3' />
            </Link>
          </div>
        )}

        {/* Security Trust Seal */}

        <TermsFooter
          variant='sign-in'
          status={status}
          className='mt-3 text-center text-xs'
        />
      </div>
    </AuthLayout>
  )
}
