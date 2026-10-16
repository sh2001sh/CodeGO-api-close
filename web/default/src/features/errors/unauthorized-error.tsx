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
import { useNavigate, useRouter } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { PublicLayout } from '@/components/layout'
import './errors.css'

export function UnauthorisedError(props: { embedded?: boolean } = {}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  const content = (
    <div
      className={
        props.embedded ? 'market-error market-error-embedded' : 'market-error'
      }
    >
      <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
        <h1 className='text-[7rem] leading-tight font-bold'>401</h1>
        <span className='font-medium'>{t('Unauthorized Access')}</span>
        <div className='mt-6 flex gap-4'>
          <Button variant='outline' onClick={() => history.go(-1)}>
            {t('Go Back')}
          </Button>
          <Button
            variant='outline'
            onClick={() => navigate({ to: '/sign-in' })}
          >
            {t('Sign in')}
          </Button>
          <Button onClick={() => navigate({ to: '/' })}>
            {t('Back to Home')}
          </Button>
        </div>
      </div>
    </div>
  )

  if (props.embedded) return content

  return (
    <PublicLayout showMainContainer={false}>
      <main id='content' tabIndex={-1}>
        {content}
      </main>
    </PublicLayout>
  )
}
