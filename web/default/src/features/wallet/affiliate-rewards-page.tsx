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
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import {
  ChevronLeft,
  ChevronRight,
  Copy,
  RefreshCw,
  Wallet,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatNumber, formatQuota, formatTimestampToDate } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SectionPageLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import {
  consumeSubscriptionResetOpportunity,
  getSelfSubscriptionFull,
} from '@/features/subscriptions/api'
import {
  getOrderedSubscriptions,
  isActiveUserSubscription,
  isCreditPackSubscription,
} from '@/features/subscriptions/lib'
import './commerce.css'
import { WalletResetOpportunityPanel } from './components/wallet-reset-opportunity-panel'
import { useAffiliate } from './hooks'

const INVITEES_PER_PAGE = 20

export function AffiliateRewardsPage() {
  const { t } = useTranslation()
  const [usingResetOpportunity, setUsingResetOpportunity] = useState(false)
  const [page, setPage] = useState(1)
  const affiliate = useAffiliate()
  const overview = affiliate.overview
  const resetOpportunity = overview?.reset_opportunity
  const hasHistoricalReset = Boolean(
    resetOpportunity &&
    (resetOpportunity.available_count > 0 ||
      resetOpportunity.earned_total > 0 ||
      resetOpportunity.used_total > 0)
  )
  const subscriptionsQuery = useQuery({
    queryKey: ['subscription', 'self', 'affiliate-rewards'],
    queryFn: getSelfSubscriptionFull,
    staleTime: 60 * 1000,
    enabled: hasHistoricalReset,
  })
  const subscriptions = subscriptionsQuery.data?.data
  const legacySubscription = useMemo(
    () =>
      getOrderedSubscriptions(
        subscriptions?.subscriptions ?? [],
        subscriptions?.subscription_order_ids ?? []
      ).find(
        (record) =>
          !isCreditPackSubscription(record.subscription) &&
          isActiveUserSubscription(record.subscription)
      ),
    [subscriptions]
  )
  const canUseResetOpportunity = Boolean(
    subscriptionsQuery.data?.success &&
    legacySubscription &&
    resetOpportunity &&
    resetOpportunity.available_count > 0 &&
    !resetOpportunity.used_this_month
  )
  const invitees = overview?.invitees ?? []
  const pageCount = Math.max(1, Math.ceil(invitees.length / INVITEES_PER_PAGE))
  const currentPage = Math.min(page, pageCount)
  const visibleInvitees = invitees.slice(
    (currentPage - 1) * INVITEES_PER_PAGE,
    currentPage * INVITEES_PER_PAGE
  )
  const metrics = [
    {
      label: t('已邀请人数'),
      value: formatNumber(overview?.invited_count ?? 0),
    },
    {
      label: t('首周实付消费'),
      value: formatQuota(overview?.first_week_consumed_quota ?? 0),
    },
    {
      label: t('已返利'),
      value: formatQuota(overview?.rebate_total_quota ?? 0),
    },
    {
      label: t('首周返利比例'),
      value: overview ? `${overview.rebate_rate_bps / 100}%` : '--',
    },
  ]

  const handleUseResetOpportunity = async () => {
    if (usingResetOpportunity || !canUseResetOpportunity) return
    setUsingResetOpportunity(true)
    try {
      const response = await consumeSubscriptionResetOpportunity()
      if (!response.success) {
        toast.error(response.message || t('额度刷新失败'))
        return
      }
      toast.success(t('已刷新旧套餐额度'))
      await Promise.all([affiliate.refetch(), subscriptionsQuery.refetch()])
      window.dispatchEvent(new Event('subscription:changed'))
    } catch {
      toast.error(t('额度刷新失败'))
    } finally {
      setUsingResetOpportunity(false)
    }
  }

  return (
    <SectionPageLayout kicker=''>
      <SiteSeo
        title={t('邀请返利')}
        description={t('邀请返利')}
        canonicalPath='/invite-rewards'
        robots='noindex,follow'
      />
      <SectionPageLayout.Title>{t('邀请返利')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button render={<Link to='/wallet' />}>
          <Wallet data-icon='inline-start' />
          {t('钱包')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='commerce-workspace commerce-affiliate'>
          <section
            className='commerce-invite-fields'
            aria-label={t('邀请链接')}
          >
            <div className='commerce-invite-field'>
              <label htmlFor='affiliate-link'>{t('邀请链接')}</label>
              <Input
                id='affiliate-link'
                value={affiliate.affiliateLink}
                readOnly
                aria-label={t('邀请链接')}
                className='min-w-0'
              />
              <Button
                variant='outline'
                disabled={!affiliate.affiliateLink}
                onClick={() => void affiliate.copyAffiliateLink()}
              >
                <Copy data-icon='inline-start' />
                {t('复制邀请链接')}
              </Button>
            </div>
            <div className='commerce-invite-field'>
              <label htmlFor='affiliate-code'>{t('邀请码')}</label>
              <Input
                id='affiliate-code'
                value={affiliate.affiliateCode}
                readOnly
                aria-label={t('邀请码')}
                className='min-w-0'
              />
              <Button
                variant='outline'
                disabled={!affiliate.affiliateCode}
                onClick={() => void affiliate.copyAffiliateCode()}
              >
                <Copy data-icon='inline-start' />
                {t('复制邀请码')}
              </Button>
            </div>
          </section>

          {affiliate.isError ? (
            <div
              role='alert'
              className='flex flex-wrap items-center justify-between gap-3 border p-4'
            >
              <span>{t('邀请记录加载失败')}</span>
              <Button
                variant='outline'
                disabled={affiliate.refreshing}
                onClick={() => void affiliate.refetch()}
              >
                <RefreshCw className='size-4' aria-hidden />
                {t('重试')}
              </Button>
            </div>
          ) : (
            <>
              <dl className='commerce-invite-metrics'>
                {metrics.map((metric) => (
                  <div key={metric.label} className='min-w-0'>
                    <dt className='text-muted-foreground text-sm'>
                      {metric.label}
                    </dt>
                    <dd className='mt-2 text-2xl font-semibold tabular-nums'>
                      {affiliate.loading ? (
                        <Skeleton className='h-8 w-24' />
                      ) : (
                        metric.value
                      )}
                    </dd>
                  </div>
                ))}
              </dl>
              <section aria-labelledby='affiliate-records-title'>
                <h2
                  id='affiliate-records-title'
                  className='mb-6 text-2xl font-semibold'
                >
                  {t('返利明细')}
                </h2>
                {affiliate.loading ? (
                  <div className='space-y-2'>
                    {Array.from({ length: 4 }, (_, index) => (
                      <Skeleton key={index} className='h-14 w-full' />
                    ))}
                  </div>
                ) : invitees.length === 0 ? (
                  <div className='commerce-empty'>{t('暂无邀请记录')}</div>
                ) : (
                  <>
                    <Table className='commerce-invite-table'>
                      <TableHeader>
                        <TableRow>
                          <TableHead>{t('用户')}</TableHead>
                          <TableHead>{t('注册时间')}</TableHead>
                          <TableHead>{t('首周截止')}</TableHead>
                          <TableHead className='text-right'>
                            {t('首周实付消费')}
                          </TableHead>
                          <TableHead className='text-right'>
                            {t('已入钱包')}
                          </TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {visibleInvitees.map((invitee) => (
                          <TableRow key={invitee.invitee_id}>
                            <TableCell>
                              <div className='max-w-56 font-medium break-words'>
                                {invitee.invitee_display_name ||
                                  invitee.invitee_username ||
                                  invitee.invitee_external_id ||
                                  t('用户 #{{param0}}', {
                                    param0: invitee.invitee_id,
                                  })}
                              </div>
                              <div className='text-muted-foreground text-xs'>
                                ID:{' '}
                                {invitee.invitee_external_id ||
                                  invitee.invitee_id}
                              </div>
                            </TableCell>
                            <TableCell className='whitespace-nowrap'>
                              {formatTimestampToDate(invitee.created_at)}
                            </TableCell>
                            <TableCell className='whitespace-nowrap'>
                              {formatTimestampToDate(invitee.rebate_ends_at)}
                            </TableCell>
                            <TableCell className='text-right tabular-nums'>
                              {formatQuota(invitee.first_week_consumed_quota)}
                            </TableCell>
                            <TableCell className='text-right tabular-nums'>
                              {formatQuota(invitee.rebate_quota)}
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                    {pageCount > 1 && (
                      <div className='mt-4 flex items-center justify-end gap-3'>
                        <span className='text-muted-foreground text-sm tabular-nums'>
                          {currentPage} / {pageCount}
                        </span>
                        <Button
                          variant='outline'
                          size='icon'
                          aria-label={t('上一页')}
                          disabled={currentPage === 1}
                          onClick={() => setPage(currentPage - 1)}
                        >
                          <ChevronLeft className='size-4' aria-hidden />
                        </Button>
                        <Button
                          variant='outline'
                          size='icon'
                          aria-label={t('下一页')}
                          disabled={currentPage === pageCount}
                          onClick={() => setPage(currentPage + 1)}
                        >
                          <ChevronRight className='size-4' aria-hidden />
                        </Button>
                      </div>
                    )}
                  </>
                )}
              </section>
            </>
          )}

          {hasHistoricalReset && resetOpportunity && (
            <WalletResetOpportunityPanel
              resetOpportunity={resetOpportunity}
              currentSubscriptionTitle={
                legacySubscription?.plan?.title ||
                (legacySubscription
                  ? t('Plan #{{id}}', {
                      id: legacySubscription.subscription.id,
                    })
                  : undefined)
              }
              canUseResetOpportunity={canUseResetOpportunity}
              usingResetOpportunity={usingResetOpportunity}
              onUseResetOpportunity={() => void handleUseResetOpportunity()}
              hideDetailsLink
            />
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
