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
import { useState } from 'react'
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
import { getSelfSubscriptionFull } from '@/features/subscriptions/api'
import './commerce.css'
import { WalletResetOpportunityPanel } from './components/wallet-reset-opportunity-panel'
import { useAffiliate } from './hooks'

const INVITEES_PER_PAGE = 20

export function AffiliateRewardsPage() {
  const { t } = useTranslation()
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

          <section
            className='commerce-invite-rules'
            aria-labelledby='affiliate-rules-title'
          >
            <h2 id='affiliate-rules-title'>{t('返利规则')}</h2>
            <dl>
              <div>
                <dt>{t('邀请关系')}</dt>
                <dd>
                  {t(
                    '受邀人通过你的邀请链接或邀请码完成注册，并成功绑定邀请关系。'
                  )}
                </dd>
              </div>
              <div>
                <dt>{t('有效时间')}</dt>
                <dd>
                  {t(
                    '从受邀人注册时刻起连续 7 天（168 小时），按消费结算时间判定；满 7 天后的消费不计入。'
                  )}
                </dd>
              </div>
              <div>
                <dt>{t('计算方式')}</dt>
                <dd>
                  {t(
                    '返利 = 首周已结算的合资格实付消费 × {{rate}}。充值、购买套餐和转账本身不产生返利。',
                    {
                      rate: overview
                        ? `${overview.rebate_rate_bps / 100}%`
                        : '3%',
                    }
                  )}
                </dd>
              </div>
              <div>
                <dt>{t('实付范围')}</dt>
                <dd>
                  {t(
                    '钱包现金充值及现金购买的新套餐，仅按已消费额度中的实付部分计算。赠送额度、返利余额再次消费、旧套餐、旧套餐转入的钱包余额及集享增额，以及缺少成功付款记录的历史转换套餐均不计入。'
                  )}
                </dd>
              </div>
              <div>
                <dt>{t('到账方式')}</dt>
                <dd>
                  {t(
                    '消费结算完成后自动处理，返利计入邀请人的可用钱包，无需领取。处理完成前，返利明细可能稍后更新。'
                  )}
                </dd>
              </div>
              <div>
                <dt>{t('计算精度')}</dt>
                <dd>
                  {t(
                    '按每位受邀人的累计合资格消费，以最小计费单位计算返利；尾差随该受邀人后续合资格消费累计。'
                  )}
                </dd>
              </div>
              <div>
                <dt>{t('生效范围')}</dt>
                <dd>
                  {t(
                    '仅计算规则生效后的消费，历史消费不补发。邀请活动已停止新增旧套餐刷新次数。'
                  )}
                </dd>
              </div>
              {overview && overview.rebate_effective_at > 0 && (
                <div>
                  <dt>{t('规则生效时间')}</dt>
                  <dd>{formatTimestampToDate(overview.rebate_effective_at)}</dd>
                </div>
              )}
            </dl>
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
              subscriptionData={subscriptions}
              loading={subscriptionsQuery.isLoading}
              onRefresh={async () => {
                await Promise.all([
                  affiliate.refetch(),
                  subscriptionsQuery.refetch(),
                ])
              }}
            />
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
