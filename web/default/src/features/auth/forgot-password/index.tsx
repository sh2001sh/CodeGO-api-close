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
import { Link } from '@tanstack/react-router'
import i18n from '@/i18n/config'
import { ArrowLeft } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { DEFAULT_LOGO } from '@/lib/constants'
import { SiteSeo } from '@/components/seo'
import { AuthLayout } from '../auth-layout'
import { ForgotPasswordForm } from './components/forgot-password-form'

export function ForgotPassword() {
  const { t } = useTranslation()

  return (
    <AuthLayout>
      <SiteSeo
        title={i18n.t('找回密码')}
        description={i18n.t(
          '找回 Code Go 账户密码并重新访问你的控制台、API Key 与模型配置。'
        )}
        canonicalPath='/forgot-password'
        robots='noindex,follow'
      />
      <div className='w-full'>
        {/* Top Brand Logo & Title */}
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
          <h1 className='market-auth-title'>{t('Forgot password')}</h1>
        </div>

        <ForgotPasswordForm className='space-y-0' />

        <div className='market-auth-switch-row'>
          <Link to='/sign-in' className='market-auth-switch-link'>
            <ArrowLeft className='mr-1 inline-block size-3' />
            {t('Back to sign in')}
          </Link>
        </div>
      </div>
    </AuthLayout>
  )
}
