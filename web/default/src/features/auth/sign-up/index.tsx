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
import { useStatus } from '@/hooks/use-status'
import { SiteSeo } from '@/components/seo'
import { AuthLayout } from '../auth-layout'
import { TermsFooter } from '../components/terms-footer'
import { SignUpForm } from './components/sign-up-form'

export function SignUp() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const { redirect } = useSearch({ from: '/(auth)/sign-up' })

  return (
    <AuthLayout>
      <SiteSeo
        title={i18n.t('注册')}
        description={i18n.t(
          '注册 Code Go 以创建账户并开始使用 API、模型广场与长期 AI Coding 工作流。'
        )}
        canonicalPath='/sign-up'
        robots='noindex,follow'
      />
      <div className='w-full'>
        {/* Top Brand Logo & Title (Vercel & OpenDesign style) */}
        <div className='market-auth-header'>
          <div className='market-auth-brand-emblem' aria-hidden='true'>
            <img
              src='/codego-mark.svg'
              alt=''
              width='32'
              height='32'
              className='size-8 object-contain'
            />
          </div>
          <h1 className='market-auth-title'>{t('Create an account')}</h1>
        </div>

        <SignUpForm redirectTo={redirect} />

        <div className='market-auth-switch-row'>
          <span>{t('Already have an account?')}</span>
          <Link
            to='/sign-in'
            search={redirect ? { redirect } : {}}
            className='market-auth-switch-link'
          >
            {t('Sign in')}
            <ArrowRight className='ml-1 inline-block size-3' />
          </Link>
        </div>

        <TermsFooter
          variant='sign-up'
          status={status}
          className='mt-3 text-center text-xs'
        />
      </div>
    </AuthLayout>
  )
}
