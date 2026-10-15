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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import { formatSubscriptionPlanTitle } from '@/features/subscriptions/lib'
import { getMyGroupBuys } from './api'

export function GroupBuyRecords() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['group-buy', 'mine'],
    queryFn: async () => {
      const response = await getMyGroupBuys()
      if (!response.success || !response.data)
        throw new Error(
          response.message || t('Unable to load participation records.')
        )
      return response
    },
  })
  return (
    <section id='group-buy' className='min-w-0'>
      <TitledCard title={t('我的参与')} description={t('结算记录')}>
        {query.isLoading ? (
          <Skeleton className='h-24' />
        ) : query.isError ? (
          <div role='alert'>
            <p>{t('Unable to load participation records.')}</p>
            <Button variant='outline' onClick={() => void query.refetch()}>
              {t('Try again')}
            </Button>
          </div>
        ) : query.data?.data?.data?.length ? (
          <div className='divide-y'>
            {query.data.data.data.map((item) => (
              <div
                key={item.id}
                className='flex flex-wrap items-center justify-between gap-3 py-3 text-sm'
              >
                <span>{formatSubscriptionPlanTitle(item.plan_name)}</span>
                <span>
                  {item.current_count} / {item.target_count}
                </span>
                <span>
                  {t(item.status === 'pending' ? '本期进行中' : '本期已结算')}
                </span>
              </div>
            ))}
          </div>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t(
              '暂无参与记录。购买集享套餐后，本期进度和最终结算结果会显示在这里。'
            )}
          </p>
        )}
      </TitledCard>
    </section>
  )
}
