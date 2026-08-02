import { useState, type ReactNode } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { SectionPageLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import { BountyBalanceSummary } from './components/bounty-balance-summary'
import { BountyFilterBar } from './components/bounty-filter-bar'
import { BountyList } from './components/bounty-list'
import { BountyNotificationPanel } from './components/bounty-notification-panel'
import { BountyPublishDrawer } from './components/bounty-publish-drawer'
import { useBountyBalances, useBountyList } from './hooks/use-bounty-list'
import type { BountySearch, BountyTask } from './types'

interface BountyMarketProps {
  search: BountySearch
  onSearchChange: (next: Partial<BountySearch>) => void
}

export function BountyMarket(props: BountyMarketProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const user = useAuthStore((state) => state.auth.user)
  const [publishOpen, setPublishOpen] = useState(false)
  const [editingDraft, setEditingDraft] = useState<BountyTask>()
  const balanceQuery = useBountyBalances(Boolean(user))
  const effectiveSearch = user
    ? props.search
    : {
        ...props.search,
        scope: undefined,
        status:
          props.search.status === 'draft' || props.search.status === 'suspended'
            ? undefined
            : props.search.status,
      }
  const listQuery = useBountyList(effectiveSearch)
  const openPublish = () => {
    if (!user) {
      void navigate({ to: '/sign-in', search: { redirect: '/bounties' } })
      return
    }
    setEditingDraft(undefined)
    setPublishOpen(true)
  }
  const scopes = user
    ? [
        ['all', t('Task hall')],
        ['mine_published', t('Published by me')],
        ['mine_assigned', t('Assigned to me')],
        ['mine_disputes', t('My disputes')],
      ]
    : [['all', t('Task hall')]]

  return (
    <>
      <SectionPageLayout>
        <SiteSeo
          title={t('Task bounty market')}
          description={t(
            'Exchange verified GitHub delivery for platform quota.'
          )}
          canonicalPath='/bounties'
          robots='noindex,follow'
        />
        <SectionPageLayout.Title>
          {t('Task bounty market')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t(
            'Use GitHub delivery to exchange coding work for normal or Claude quota.'
          )}
        </SectionPageLayout.Description>
        <SectionPageLayout.Actions>
          {user ? <BountyNotificationPanel /> : null}
          <Button
            render={
              user ? undefined : (
                <Link to='/sign-in' search={{ redirect: '/bounties' }} />
              )
            }
            onClick={user ? openPublish : undefined}
          >
            <Plus aria-hidden='true' />
            {user ? t('Publish a task') : t('Sign in to publish a task')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-[1440px] flex-col gap-4'>
            {user ? (
              <BountyBalanceSummary
                balances={balanceQuery.data ?? []}
                loading={balanceQuery.isLoading}
                error={balanceQuery.isError}
              />
            ) : null}
            <div
              className='border-border/70 bg-card/60 flex flex-wrap items-center gap-1 rounded-xl border p-1'
              role='tablist'
              aria-label={t('Bounty task views')}
            >
              {scopes.map(([value, label]) => (
                <Button
                  key={value}
                  variant={
                    effectiveSearch.scope === value ||
                    (!effectiveSearch.scope && value === 'all')
                      ? 'secondary'
                      : 'ghost'
                  }
                  size='sm'
                  role='tab'
                  aria-selected={
                    effectiveSearch.scope === value ||
                    (!effectiveSearch.scope && value === 'all')
                  }
                  onClick={() =>
                    props.onSearchChange({
                      scope: value as BountySearch['scope'],
                      page: 1,
                    })
                  }
                >
                  {label}
                </Button>
              ))}
            </div>
            <BountyFilterBar
              search={props.search}
              onSearchChange={props.onSearchChange}
            />
            <BountyList
              result={listQuery.data}
              loading={listQuery.isLoading}
              error={listQuery.error}
              onClearFilters={() =>
                props.onSearchChange({
                  keyword: '',
                  tag: '',
                  wallet_type: 'all',
                  status: 'all',
                  sort: 'latest',
                  min_reward: undefined,
                  max_reward: undefined,
                  page: 1,
                })
              }
              onPublish={openPublish}
              onEdit={(task) => {
                setEditingDraft(task)
                setPublishOpen(true)
              }}
            />
            {listQuery.data &&
            listQuery.data.total > listQuery.data.page_size ? (
              <div className='flex items-center justify-between text-sm'>
                <span className='text-muted-foreground'>
                  {t('Showing {{count}} tasks', {
                    count: listQuery.data.total,
                  })}
                </span>
                <div className='flex gap-2'>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={(props.search.page ?? 1) <= 1}
                    onClick={() =>
                      props.onSearchChange({
                        page: Math.max((props.search.page ?? 1) - 1, 1),
                      })
                    }
                  >
                    {t('Previous')}
                  </Button>
                  <Button
                    variant='outline'
                    size='sm'
                    disabled={
                      listQuery.data.total <=
                      (props.search.page ?? 1) * listQuery.data.page_size
                    }
                    onClick={() =>
                      props.onSearchChange({
                        page: (props.search.page ?? 1) + 1,
                      })
                    }
                  >
                    {t('Next')}
                  </Button>
                </div>
              </div>
            ) : null}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
      {publishOpen ? (
        <BountyPublishDrawer
          key={editingDraft?.task_id ?? 'new'}
          open
          task={editingDraft}
          onOpenChange={(open) => {
            setPublishOpen(open)
            if (!open) setEditingDraft(undefined)
          }}
        />
      ) : null}
    </>
  )
}

export function BountyDetailRouteLink(props: {
  taskId: string
  children: ReactNode
}) {
  return (
    <Link to='/bounties/$taskId' params={{ taskId: props.taskId }}>
      {props.children}
    </Link>
  )
}
