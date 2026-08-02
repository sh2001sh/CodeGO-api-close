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
import { useMemo, useState, type ComponentType } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Copy, Gift, RotateCcw, Sparkles, Users, Wallet } from 'lucide-react'
import { motion, useReducedMotion, type Variants } from 'motion/react'
import { toast } from 'sonner'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { MOTION_TRANSITION } from '@/lib/motion'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
import { useAffiliate } from './hooks'
import type { AffiliateInviteeRewardStatus } from './types'

const CARDS_STAGGER: Variants = {
  hidden: {},
  visible: { transition: { staggerChildren: 0.08, delayChildren: 0.1 } },
}

const CARD_ITEM: Variants = {
  hidden: { opacity: 0, y: 14, scale: 0.97 },
  visible: { opacity: 1, y: 0, scale: 1, transition: MOTION_TRANSITION.slow },
}

const SECTION_FADE: Variants = {
  hidden: { opacity: 0, y: 20 },
  visible: { opacity: 1, y: 0, transition: MOTION_TRANSITION.slow },
}

function StatCard(props: {
  title: string
  value: string
  hint: string
  icon: ComponentType<{ className?: string }>
  iconClass?: string
}) {
  const Icon = props.icon

  return (
    <motion.div variants={CARD_ITEM} className='overview-soft-card px-4 py-4'>
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0 flex-1'>
          <div className='text-muted-foreground text-[11px] font-medium tracking-wide uppercase'>
            {props.title}
          </div>
          <div className='mt-2 text-2xl font-semibold tracking-tight'>
            {props.value}
          </div>
          <div className='text-muted-foreground mt-1 text-xs leading-5'>
            {props.hint}
          </div>
        </div>
        <div
          className={cn(
            'flex size-9 shrink-0 items-center justify-center rounded-xl',
            props.iconClass ?? 'bg-muted text-muted-foreground'
          )}
        >
          <Icon className='size-4' />
        </div>
      </div>
    </motion.div>
  )
}

function StatusBadge(props: {
  completed: boolean
  doneText: string
  todoText: string
}) {
  return (
    <Badge variant={props.completed ? 'default' : 'outline'}>
      {props.completed ? props.doneText : props.todoText}
    </Badge>
  )
}

function getInviteeName(invitee: AffiliateInviteeRewardStatus) {
  return (
    invitee.invitee_display_name ||
    invitee.invitee_username ||
    invitee.invitee_external_id ||
    `用户 #${invitee.invitee_id}`
  )
}

export function AffiliateRewardsPage() {
  const shouldReduceMotion = Boolean(useReducedMotion())
  const [usingResetOpportunity, setUsingResetOpportunity] = useState(false)
  const {
    overview,
    affiliateCode,
    affiliateLink,
    loading,
    copyAffiliateCode,
    copyAffiliateLink,
    refetch,
  } = useAffiliate()
  const subscriptionsQuery = useQuery({
    queryKey: ['subscription', 'self', 'affiliate-rewards'],
    queryFn: getSelfSubscriptionFull,
    staleTime: 60 * 1000,
  })

  const resetOpportunity = overview?.reset_opportunity ?? {
    available_count: 0,
    earned_total: 0,
    used_total: 0,
    used_this_month: false,
    current_month: '',
    last_used_month: '',
  }

  const inviteeRows = useMemo(
    () =>
      (overview?.invitees ?? []).map((invitee) => ({
        ...invitee,
        name: getInviteeName(invitee),
      })),
    [overview?.invitees]
  )
  const hasActiveSubscription = Boolean(
    subscriptionsQuery.data?.success &&
    subscriptionsQuery.data.data?.subscriptions.length
  )
  const resetStatus = resetOpportunity.used_this_month
    ? '已使用'
    : !hasActiveSubscription
      ? '暂无生效套餐'
      : resetOpportunity.available_count <= 0
        ? '暂无机会'
        : '可刷新'
  const canUseResetOpportunity =
    hasActiveSubscription &&
    resetOpportunity.available_count > 0 &&
    !resetOpportunity.used_this_month

  const handleUseResetOpportunity = async () => {
    if (usingResetOpportunity || !canUseResetOpportunity) {
      return
    }
    setUsingResetOpportunity(true)
    try {
      const response = await consumeSubscriptionResetOpportunity()
      if (!response.success) {
        toast.error(response.message || '额度刷新失败')
        return
      }
      toast.success('已刷新当前订阅额度')
      await refetch()
      if (typeof window !== 'undefined') {
        window.dispatchEvent(new Event('subscription:changed'))
      }
    } catch {
      toast.error('额度刷新失败')
    } finally {
      setUsingResetOpportunity(false)
    }
  }

  return (
    <SectionPageLayout>
      <SiteSeo
        title='邀请与刷新'
        description='分享邀请链接，获得套餐额度刷新机会并查看奖励记录。'
        canonicalPath='/invite-rewards'
        robots='noindex,follow'
      />
      <SectionPageLayout.Title>邀请与刷新</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button variant='outline' render={<Link to='/packages' />}>
          套餐
        </Button>
        <Button render={<Link to='/wallet' />}>
          <Wallet data-icon='inline-start' />
          钱包
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-4'>
          <motion.div
            variants={SECTION_FADE}
            initial={shouldReduceMotion ? false : 'hidden'}
            animate='visible'
          >
            <Card className='overflow-hidden py-0'>
              <CardContent className='bg-card grid gap-4 p-4 sm:p-5 lg:grid-cols-[minmax(0,1.2fr)_minmax(320px,0.8fr)]'>
                <div className='min-w-0'>
                  <div className='app-section-kicker'>邀请与刷新</div>
                  <div className='border-border bg-background/80 text-foreground mt-3 inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-semibold'>
                    <Sparkles className='h-3.5 w-3.5' />
                    邀请人首购月卡奖励：1 次额度刷新机会
                  </div>
                  <div className='mt-2 text-2xl font-semibold tracking-tight sm:text-3xl'>
                    邀请新用户首购月卡，给你 1 次额度刷新机会
                  </div>
                  <div className='text-muted-foreground mt-2 max-w-2xl text-sm leading-6'>
                    通过你的邀请链接注册的新用户，首次购买月卡成功后， 你会获得
                    1
                    次可长期保留的额度刷新机会。刷新机会可直接清空当前订阅的已用额度，
                    适合高频使用时做一次完整恢复。
                  </div>

                  <div className='mt-4 grid gap-3 sm:grid-cols-3'>
                    <div className='app-subtle-panel px-4 py-4'>
                      <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
                        <Users className='text-primary h-4 w-4' />
                        1. 分享专属链接
                      </div>
                      <div className='text-muted-foreground mt-2 text-sm leading-6'>
                        新用户必须通过你的专属链接完成首次注册。
                      </div>
                    </div>
                    <div className='app-subtle-panel px-4 py-4'>
                      <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
                        <Gift className='text-primary h-4 w-4' />
                        2. 好友首购月卡
                      </div>
                      <div className='text-muted-foreground mt-2 text-sm leading-6'>
                        新用户注册后可获得少量积分，首购月卡才触发你的奖励。
                      </div>
                    </div>
                    <div className='app-subtle-panel px-4 py-4'>
                      <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
                        <RotateCcw className='text-primary h-4 w-4' />
                        3. 获得刷新机会
                      </div>
                      <div className='text-muted-foreground mt-2 text-sm leading-6'>
                        你增加 1 次额度刷新机会，可长期保留，但每月最多使用 1
                        次。
                      </div>
                    </div>
                  </div>

                  <div className='mt-4 grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]'>
                    {loading ? (
                      <Skeleton className='h-11 rounded-xl' />
                    ) : (
                      <Input
                        value={affiliateLink}
                        readOnly
                        className='h-11 font-mono text-xs sm:text-sm'
                      />
                    )}
                    <Button
                      onClick={() => void copyAffiliateLink()}
                      disabled={!affiliateLink}
                      className='h-11'
                    >
                      <Copy data-icon='inline-start' />
                      复制邀请链接
                    </Button>
                  </div>
                  <div className='mt-2 grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]'>
                    {loading ? (
                      <Skeleton className='h-10 rounded-xl' />
                    ) : (
                      <Input
                        value={affiliateCode}
                        readOnly
                        aria-label='邀请码'
                        className='h-10 font-mono text-sm'
                      />
                    )}
                    <Button
                      variant='outline'
                      onClick={() => void copyAffiliateCode()}
                      disabled={!affiliateCode}
                      className='h-10'
                    >
                      <Copy data-icon='inline-start' />
                      复制邀请码
                    </Button>
                  </div>
                </div>

                <div className='app-page-shell p-4'>
                  <div className='text-foreground flex items-center gap-2 text-sm font-semibold'>
                    <Sparkles className='text-warning h-4 w-4' />
                    关键规则
                  </div>
                  <div className='text-muted-foreground mt-2 space-y-2 text-sm leading-6'>
                    <p>1. 刷新只影响当前排序第 1 个生效订阅。</p>
                    <p>2. 会清空已用额度与周期已用值，但不会延长到期时间。</p>
                    <p>3. 刷新机会可累计保留，每个自然月最多使用 1 次。</p>
                  </div>
                </div>
              </CardContent>
            </Card>
          </motion.div>

          {loading ? (
            <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
              {Array.from({ length: 4 }).map((_, index) => (
                <Skeleton key={index} className='h-32 rounded-xl' />
              ))}
            </div>
          ) : (
            <motion.div
              className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'
              variants={CARDS_STAGGER}
              initial={shouldReduceMotion ? false : 'hidden'}
              whileInView='visible'
              viewport={{ once: true, margin: '-40px' }}
            >
              <StatCard
                title='已邀请人数'
                value={formatNumber(overview?.invited_count ?? 0)}
                hint='通过你的链接完成注册的用户数'
                icon={Users}
                iconClass='bg-muted text-muted-foreground'
              />
              <StatCard
                title='月卡首购完成'
                value={formatNumber(overview?.successful_purchase_invites ?? 0)}
                hint='已经为你触发刷新机会的人数'
                icon={Sparkles}
                iconClass='bg-primary/10 text-primary'
              />
              <StatCard
                title='可刷新次数'
                value={formatNumber(resetOpportunity.available_count)}
                hint='当前还能使用的刷新机会'
                icon={RotateCcw}
                iconClass='bg-success/10 text-success'
              />
              <StatCard
                title='本月状态'
                value={resetStatus}
                hint='每个自然月最多刷新 1 次'
                icon={Wallet}
                iconClass='bg-muted text-muted-foreground'
              />
            </motion.div>
          )}

          <motion.div
            className='grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(340px,0.8fr)]'
            variants={SECTION_FADE}
            initial={shouldReduceMotion ? false : 'hidden'}
            whileInView='visible'
            viewport={{ once: true, margin: '-40px' }}
          >
            <Card className='py-0'>
              <CardHeader>
                <CardTitle>邀请明细</CardTitle>
                <CardDescription>
                  查看每位被邀请人的月卡首购状态，以及对应的刷新机会发放情况。
                </CardDescription>
              </CardHeader>
              <CardContent className='pb-4'>
                {loading ? (
                  <div className='space-y-2'>
                    {Array.from({ length: 4 }).map((_, index) => (
                      <Skeleton key={index} className='h-14 rounded-xl' />
                    ))}
                  </div>
                ) : inviteeRows.length === 0 ? (
                  <div className='text-muted-foreground rounded-xl border border-dashed px-4 py-10 text-center text-sm'>
                    还没有拉新记录。先复制邀请链接发给新用户。
                  </div>
                ) : (
                  <div className='overflow-hidden rounded-xl border'>
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>用户</TableHead>
                          <TableHead>注册时间</TableHead>
                          <TableHead>月卡首购</TableHead>
                          <TableHead>刷新机会</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {inviteeRows.map((invitee) => (
                          <TableRow key={invitee.invitee_id}>
                            <TableCell>
                              <div className='flex min-w-0 flex-col'>
                                <span className='font-medium'>
                                  {invitee.name}
                                </span>
                                <span className='text-muted-foreground text-xs'>
                                  ID:{' '}
                                  {invitee.invitee_external_id ||
                                    invitee.invitee_id}
                                </span>
                              </div>
                            </TableCell>
                            <TableCell>
                              {formatTimestampToDate(invitee.created_at)}
                            </TableCell>
                            <TableCell>
                              <StatusBadge
                                completed={invitee.month_card_purchased}
                                doneText='已购买'
                                todoText='未购买'
                              />
                            </TableCell>
                            <TableCell>
                              <div className='flex flex-col gap-1'>
                                <StatusBadge
                                  completed={invitee.reset_opportunity_earned}
                                  doneText='已发放'
                                  todoText='未发放'
                                />
                                {invitee.reset_opportunity_earned_at ? (
                                  <span className='text-muted-foreground text-xs'>
                                    {formatTimestampToDate(
                                      invitee.reset_opportunity_earned_at
                                    )}
                                  </span>
                                ) : null}
                              </div>
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                )}
              </CardContent>
            </Card>

            <div className='flex flex-col gap-4'>
              <div className='overview-glass-card rounded-2xl p-5'>
                <div className='text-base font-semibold'>立即刷新</div>
                <div className='text-muted-foreground mt-1 text-sm leading-6'>
                  使用刷新机会清空当前订阅的已用额度。本月已刷新或暂无机会时不可用。
                </div>
                <div className='mt-4 grid gap-2 sm:grid-cols-2'>
                  <div className='overview-soft-card px-3 py-3'>
                    <div className='text-muted-foreground text-[11px] font-medium tracking-wide uppercase'>
                      可用次数
                    </div>
                    <div className='mt-1.5 text-2xl font-semibold tabular-nums'>
                      {resetOpportunity.available_count}
                    </div>
                  </div>
                  <div className='overview-soft-card px-3 py-3'>
                    <div className='text-muted-foreground text-[11px] font-medium tracking-wide uppercase'>
                      本月状态
                    </div>
                    <div
                      className={cn(
                        'mt-1.5 text-2xl font-semibold tabular-nums',
                        canUseResetOpportunity
                          ? 'text-success'
                          : 'text-muted-foreground'
                      )}
                    >
                      {resetStatus}
                    </div>
                  </div>
                </div>
                <div className='mt-4 flex flex-col gap-2'>
                  <Button
                    className='w-full'
                    onClick={() => void handleUseResetOpportunity()}
                    disabled={usingResetOpportunity || !canUseResetOpportunity}
                  >
                    立即刷新当前订阅额度
                  </Button>
                  <Button
                    variant='outline'
                    className='w-full'
                    render={<Link to='/wallet' />}
                  >
                    去钱包查看当前订阅
                  </Button>
                </div>
              </div>

              <div className='overview-glass-card rounded-2xl p-5'>
                <div className='text-base font-semibold'>你会刷新什么</div>
                <div className='text-muted-foreground mt-1 text-sm leading-6'>
                  刷新只影响当前排序第 1 个生效订阅，具体排序可在钱包页调整。
                </div>
                <ul className='text-muted-foreground mt-3 space-y-2 text-sm leading-6'>
                  <li className='flex items-start gap-2'>
                    <span className='bg-foreground/10 text-foreground mt-1 inline-flex size-4 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold'>
                      1
                    </span>
                    清空当前订阅的已用总额度。
                  </li>
                  <li className='flex items-start gap-2'>
                    <span className='bg-foreground/10 text-foreground mt-1 inline-flex size-4 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold'>
                      2
                    </span>
                    如果有周期额度，也会一起清空周期已用值。
                  </li>
                  <li className='flex items-start gap-2'>
                    <span className='bg-foreground/10 text-foreground mt-1 inline-flex size-4 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold'>
                      3
                    </span>
                    不延长套餐到期时间，不增加总额度，不改变权益组。
                  </li>
                </ul>
              </div>
            </div>
          </motion.div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
