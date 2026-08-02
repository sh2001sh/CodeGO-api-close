import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertCircle, RefreshCw, Save } from 'lucide-react'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { SectionPageLayout } from '@/components/layout'
import { RoutePoolInsights } from './components/route-pool-insights'
import { RoutePoolGroupList } from './components/route-pool-group-list'
import type { FundingPolicy, RoutePoolGroup, RoutePoolMetrics } from './types'

type DailyEconomics = {
  recognized_revenue: number
  recognized_profit: number
  sources: Array<{ source: string; quota: number; profit: number }>
}

const fundingSources: FundingPolicy['source'][] = [
  'topup',
  'blind_box',
  'subscription',
]

export function RoutePools() {
  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>渠道与智能路由</SectionPageLayout.Title>
      <SectionPageLayout.Description>
        渠道分组来自渠道配置；启用算法后按成本、成功率、冷却和首字耗时自动选择。
      </SectionPageLayout.Description>
      <SectionPageLayout.Content>
        <RoutePoolsContent />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

export function RoutePoolsContent() {
  const queryClient = useQueryClient()
  const [model, setModel] = useState('')
  const [selectedGroup, setSelectedGroup] = useState<string | null>(null)
  const [policyOverrides, setPolicyOverrides] = useState<
    Partial<Record<FundingPolicy['source'], number>>
  >({})
  const groups = useQuery({
    queryKey: ['route-pool-groups'],
    queryFn: async () =>
      (
        await api.get<{ data: { items: RoutePoolGroup[] } }>(
          '/api/route-pools/groups'
        )
      ).data.data.items,
  })
  const selected = useMemo(
    () =>
      groups.data?.find((group) => group.group === selectedGroup) ??
      groups.data?.[0],
    [groups.data, selectedGroup]
  )
  const metrics = useQuery({
    queryKey: ['route-pool-metrics', selected?.pool_id, model],
    enabled: Boolean(selected?.pool_id && selected?.algorithm_active && model),
    queryFn: async () =>
      (
        await api.get<{ data: RoutePoolMetrics }>(
          `/api/route-pools/${selected?.pool_id}/metrics`,
          { params: { model } }
        )
      ).data.data,
  })
  const policies = useQuery({
    queryKey: ['route-finance-policies'],
    queryFn: async () =>
      (
        await api.get<{ data: { items: FundingPolicy[] } }>(
          '/api/route-finance/policies'
        )
      ).data.data.items,
  })
  const daily = useQuery({
    queryKey: ['route-finance-daily'],
    queryFn: async () =>
      (await api.get<{ data: DailyEconomics }>('/api/route-finance/daily')).data
        .data,
  })
  const saveGroup = useMutation({
    mutationFn: (group: RoutePoolGroup) =>
      api.put('/api/route-pools/groups', {
        group: group.group,
        enabled: group.enabled,
        members: group.channels.map((channel) => ({
          channel_id: channel.channel_id,
          enabled: channel.enabled,
          cost_multiplier: channel.cost_multiplier,
          model_cost_overrides: channel.model_cost_overrides,
          fault_domain: channel.fault_domain,
        })),
      }),
    onSuccess: () => {
      toast.success('自动路由配置已保存')
      void queryClient.invalidateQueries({ queryKey: ['route-pool-groups'] })
      void queryClient.invalidateQueries({ queryKey: ['route-pool-metrics'] })
    },
    onError: () => toast.error('自动路由配置保存失败'),
  })
  const savePolicies = useMutation({
    mutationFn: (items: FundingPolicy[]) =>
      api.put('/api/route-finance/policies', { policies: items }),
    onSuccess: () => {
      toast.success('来源倍率已保存')
      void queryClient.invalidateQueries({ queryKey: ['route-finance-policies'] })
    },
    onError: () => toast.error('来源倍率保存失败'),
  })
  const policyDraft = useMemo(
    () =>
      fundingSources.map((source) => ({
        source,
        revenue_multiplier:
          policyOverrides[source] ??
          policies.data?.find((policy) => policy.source === source)
            ?.revenue_multiplier ??
          0,
      })),
    [policies.data, policyOverrides]
  )
  const refresh = () => {
    void groups.refetch()
    void policies.refetch()
    void daily.refetch()
    void metrics.refetch()
  }
  const queryFailed = groups.isError || policies.isError || daily.isError

  return (
    <div className='space-y-4'>
      <div className='flex flex-col justify-between gap-3 sm:flex-row sm:items-start'>
        <div>
          <h2 className='text-lg font-semibold'>自动路由</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            分组与候选渠道自动同步自渠道配置。开启后，未命中缓存粘性的请求按成本、健康度和首字时间评分选择。
          </p>
        </div>
        <Button variant='outline' onClick={refresh}>
          <RefreshCw />
          刷新
        </Button>
      </div>

      {queryFailed && (
        <Card className='border-destructive/40'>
          <CardContent className='flex items-center justify-between gap-3 py-4'>
            <div className='flex min-w-0 items-start gap-2 text-sm'>
              <AlertCircle className='text-destructive mt-0.5 size-4 shrink-0' />
              <div>
                <p className='font-medium'>路由数据暂时无法加载</p>
                <p className='text-muted-foreground mt-1'>
                  请刷新重试；现有渠道和运行中的路由不会被修改。
                </p>
              </div>
            </div>
            <Button size='sm' variant='outline' onClick={refresh}>
              重试
            </Button>
          </CardContent>
        </Card>
      )}

      <RoutePoolGroupList
        groups={groups.data ?? []}
        loading={groups.isLoading}
        savingGroup={saveGroup.variables?.group}
        onSelectGroup={setSelectedGroup}
        onSave={(group) => saveGroup.mutate(group)}
      />

      <div className='grid gap-4 xl:grid-cols-[minmax(0,1fr)_360px]'>
        <RoutePoolInsights
          groups={groups.data ?? []}
          model={model}
          selectedGroup={selected?.group ?? null}
          metrics={metrics.data}
          metricsLoading={metrics.isFetching}
          algorithmActive={selected?.algorithm_active ?? false}
          onModelChange={setModel}
          onGroupChange={setSelectedGroup}
        />
        <Card>
          <CardContent className='space-y-3 pt-6'>
            <h3 className='font-semibold'>用户来源倍率</h3>
            <p className='text-muted-foreground text-sm'>
              仅影响新入账额度的收益归因，不影响渠道采购倍率。
            </p>
            {policyDraft.map((policy, index) => (
              <label
                key={policy.source}
                className='grid grid-cols-[1fr_112px] items-center gap-2 text-sm'
              >
                <span>{policy.source}</span>
                <input
                  className='border-input h-9 w-full rounded-md border bg-transparent px-3 font-mono text-sm'
                  type='number'
                  min='0'
                  step='0.0001'
                  value={policy.revenue_multiplier}
                  onChange={(event) => {
                    const next = [...policyDraft]
                    next[index] = {
                      ...policy,
                      revenue_multiplier: Number(event.target.value),
                    }
                    setPolicyOverrides(
                      Object.fromEntries(
                        next.map((item) => [
                          item.source,
                          item.revenue_multiplier,
                        ])
                      )
                    )
                  }}
                />
              </label>
            ))}
            <Button
              className='w-full'
              variant='outline'
              onClick={() => savePolicies.mutate(policyDraft)}
              disabled={savePolicies.isPending}
            >
              <Save />
              保存来源倍率
            </Button>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
