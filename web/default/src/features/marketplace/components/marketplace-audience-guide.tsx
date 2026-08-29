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
import { ArrowUpRight, Compass, WalletCards } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export function MarketplaceAudienceGuide(props: {
  onBrowse: () => void
  onManage: () => void
}) {
  const { t } = useTranslation()

  return (
    <section
      className='codego-fact-row grid grid-cols-1 sm:grid-cols-2'
      aria-label={t('分组市场使用路径')}
    >
      <button
        type='button'
        onClick={props.onBrowse}
        className='group flex items-center justify-between gap-4 px-0 py-4 text-left sm:px-5 sm:first:pl-0'
      >
        <span className='flex min-w-0 items-center gap-3'>
          <Compass className='text-primary size-4 shrink-0' />
          <span className='text-foreground text-sm font-semibold'>
            {t('我是使用者')}
          </span>
        </span>
        <ArrowUpRight className='text-muted-foreground size-4 shrink-0 transition-colors group-hover:text-primary' />
      </button>
      <button
        type='button'
        onClick={props.onManage}
        className='group flex items-center justify-between gap-4 px-0 py-4 text-left sm:px-5'
      >
        <span className='flex min-w-0 items-center gap-3'>
          <WalletCards className='text-primary size-4 shrink-0' />
          <span className='text-foreground text-sm font-semibold'>
            {t('我是渠道主')}
          </span>
        </span>
        <ArrowUpRight className='text-muted-foreground size-4 shrink-0 transition-colors group-hover:text-primary' />
      </button>
    </section>
  )
}
