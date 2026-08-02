import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Gift, Plus, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { SectionPageLayout } from '@/components/layout'
import { SiteSeo } from '@/components/seo'
import { AdminRewardSetting } from './admin-reward-setting'
import {
  getCommunityResourceConfig,
  listAdminCommunityResources,
  listCommunityResources,
  listMyCommunityResources,
  reviewCommunityResource,
  submitCommunityResource,
  updateCommunityResourceConfig,
} from './api'
import { CommunityResourceList } from './resource-list'
import { ResourceSubmitSheet } from './resource-submit-sheet'
import type {
  CommunityResource,
  ResourceCategory,
  SubmitResourceInput,
} from './types'

type View = 'published' | 'mine' | 'admin'

export function CommunityResourcesPage() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const role = useAuthStore((state) => state.auth.user?.role ?? 0)
  const isAdmin = role >= 10
  const [view, setView] = useState<View>('published')
  const [submitOpen, setSubmitOpen] = useState(false)
  const [searchInput, setSearchInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [category, setCategory] = useState<ResourceCategory | 'all'>('all')
  const [page, setPage] = useState(1)
  const filters = {
    keyword,
    category,
    page,
    status: view === 'admin' ? ('pending' as const) : undefined,
  }

  const configQuery = useQuery({
    queryKey: ['community-resources', 'config'],
    queryFn: getCommunityResourceConfig,
  })
  const resourcesQuery = useQuery({
    queryKey: ['community-resources', view, filters],
    queryFn: () => {
      if (view === 'mine') return listMyCommunityResources(filters)
      if (view === 'admin') return listAdminCommunityResources(filters)
      return listCommunityResources(filters)
    },
    enabled: view !== 'admin' || isAdmin,
  })

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['community-resources'] })
  const submitMutation = useMutation({
    mutationFn: submitCommunityResource,
    onSuccess: () => {
      setSubmitOpen(false)
      setView('mine')
      refresh()
      toast.success(
        t(isAdmin ? 'Resource published' : 'Resource submitted for review')
      )
    },
    onError: (error: Error) => toast.error(error.message),
  })
  const reviewMutation = useMutation({
    mutationFn: (input: {
      resource: CommunityResource
      status: 'approved' | 'rejected'
      grantReward: boolean
    }) =>
      reviewCommunityResource(input.resource.id, {
        status: input.status,
        grant_reward: input.grantReward,
      }),
    onSuccess: (_, variables) => {
      refresh()
      toast.success(
        t(
          variables.status === 'approved'
            ? 'Resource approved'
            : 'Resource rejected'
        )
      )
    },
    onError: (error: Error) => toast.error(error.message),
  })
  const configMutation = useMutation({
    mutationFn: updateCommunityResourceConfig,
    onSuccess: () => {
      refresh()
      toast.success(t('Reward setting updated'))
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const views: Array<[View, string]> = [
    ['published', t('Resource library')],
    ['mine', t('My submissions')],
    ...(isAdmin
      ? ([['admin', t('Review queue')]] as Array<[View, string]>)
      : []),
  ]
  const result = resourcesQuery.data

  function changeView(next: View) {
    setView(next)
    setPage(1)
  }

  function submitSearch(event: React.FormEvent) {
    event.preventDefault()
    setKeyword(searchInput.trim())
    setPage(1)
  }

  function submitResource(input: SubmitResourceInput) {
    submitMutation.mutate(input)
  }

  return (
    <>
      <SectionPageLayout>
        <SiteSeo
          title={t('Community resources')}
          description={t(
            'Discover and share scripts, skills, and tools built around shu26.cfd.'
          )}
          canonicalPath='/community-resources'
          robots='noindex,follow'
        />
        <SectionPageLayout.Title>
          {t('Community resources')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t(
            'Download practical GitHub resources or submit your own work for review.'
          )}
        </SectionPageLayout.Description>
        <SectionPageLayout.Actions>
          <Button onClick={() => setSubmitOpen(true)}>
            <Plus />
            {t('Submit resource')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='mx-auto flex w-full max-w-[1280px] flex-col gap-4'>
            <section className='border-primary/20 bg-primary/5 flex flex-col gap-3 rounded-lg border p-4 sm:flex-row sm:items-center sm:justify-between'>
              <div className='flex items-start gap-3'>
                <Gift className='text-primary mt-0.5 size-5 shrink-0' />
                <div>
                  <h2 className='text-sm font-semibold'>
                    {t('Build in public, earn quota')}
                  </h2>
                  <p className='text-muted-foreground mt-1 max-w-3xl text-sm'>
                    {t(
                      'Thank shu26.cfd in your GitHub README, include the acknowledgement link when submitting, and an administrator can grant a one-time quota reward after verification.'
                    )}
                  </p>
                </div>
              </div>
              {configQuery.data?.reward_enabled ? (
                <span className='text-primary shrink-0 text-sm font-semibold tabular-nums'>
                  {t('{{amount}} USD reward', {
                    amount: configQuery.data.reward_usd,
                  })}
                </span>
              ) : null}
            </section>

            <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
              <div
                className='bg-muted flex w-fit max-w-full items-center gap-1 overflow-x-auto rounded-lg p-1'
                role='tablist'
                aria-label={t('Community resource views')}
              >
                {views.map(([value, label]) => (
                  <Button
                    key={value}
                    size='sm'
                    variant={view === value ? 'secondary' : 'ghost'}
                    role='tab'
                    aria-selected={view === value}
                    onClick={() => changeView(value)}
                  >
                    {label}
                  </Button>
                ))}
              </div>
              <form
                className='flex min-w-0 flex-1 gap-2 sm:max-w-xl'
                onSubmit={submitSearch}
              >
                <div className='relative min-w-0 flex-1'>
                  <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
                  <Input
                    className='pl-9'
                    value={searchInput}
                    onChange={(event) => setSearchInput(event.target.value)}
                    placeholder={t('Search resources')}
                  />
                </div>
                <NativeSelect
                  className='w-28 sm:w-36'
                  value={category}
                  onChange={(event) => {
                    setCategory(event.target.value as ResourceCategory | 'all')
                    setPage(1)
                  }}
                  aria-label={t('Filter by category')}
                >
                  <NativeSelectOption value='all'>
                    {t('All types')}
                  </NativeSelectOption>
                  <NativeSelectOption value='script'>
                    {t('Scripts')}
                  </NativeSelectOption>
                  <NativeSelectOption value='skill'>
                    {t('Skills')}
                  </NativeSelectOption>
                  <NativeSelectOption value='tool'>
                    {t('Tools')}
                  </NativeSelectOption>
                  <NativeSelectOption value='other'>
                    {t('Other')}
                  </NativeSelectOption>
                </NativeSelect>
                <Button
                  type='submit'
                  variant='outline'
                  size='icon'
                  aria-label={t('Search')}
                >
                  <Search />
                </Button>
              </form>
            </div>

            {view === 'admin' ? (
              <AdminRewardSetting
                key={configQuery.data?.reward_usd ?? 'loading'}
                config={configQuery.data}
                pending={configMutation.isPending}
                onSave={(value) => configMutation.mutate(value)}
              />
            ) : null}

            <CommunityResourceList
              items={result?.items}
              loading={resourcesQuery.isLoading}
              admin={view === 'admin'}
              reviewing={reviewMutation.isPending}
              config={configQuery.data}
              onReview={(resource, status, grantReward) =>
                reviewMutation.mutate({ resource, status, grantReward })
              }
            />

            {resourcesQuery.isError ? (
              <div className='border-destructive/30 text-destructive rounded-lg border p-4 text-sm'>
                {t('Unable to load community resources.')}{' '}
                <Button
                  variant='link'
                  className='h-auto px-1'
                  onClick={() => resourcesQuery.refetch()}
                >
                  {t('Try again')}
                </Button>
              </div>
            ) : null}
            {result && result.total > result.page_size ? (
              <div className='flex items-center justify-between text-sm'>
                <span className='text-muted-foreground'>
                  {t('{{count}} resources', { count: result.total })}
                </span>
                <div className='flex gap-2'>
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={page <= 1}
                    onClick={() =>
                      setPage((current) => Math.max(1, current - 1))
                    }
                  >
                    {t('Previous')}
                  </Button>
                  <Button
                    size='sm'
                    variant='outline'
                    disabled={page * result.page_size >= result.total}
                    onClick={() => setPage((current) => current + 1)}
                  >
                    {t('Next')}
                  </Button>
                </div>
              </div>
            ) : null}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <ResourceSubmitSheet
        open={submitOpen}
        pending={submitMutation.isPending}
        onOpenChange={setSubmitOpen}
        onSubmit={submitResource}
      />
    </>
  )
}
