import i18n from '@/i18n/config'
import { useTranslation, Trans } from 'react-i18next'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import type { RoutePoolGroup, RoutePoolMetrics } from '../types'

type RoutePoolInsightsProps = {
  groups: RoutePoolGroup[]
  model: string
  selectedGroup: string | null
  metrics?: RoutePoolMetrics
  metricsLoading: boolean
  algorithmActive: boolean
  onModelChange: (model: string) => void
  onGroupChange: (group: string) => void
}

export function RoutePoolInsights({
  groups,
  model,
  selectedGroup,
  metrics,
  metricsLoading,
  algorithmActive,
  onModelChange,
  onGroupChange,
}: RoutePoolInsightsProps) {
  useTranslation()
  return (
    <Card className='route-pool-insights'>
      <CardHeader>
        <CardTitle>
          <Trans i18nKey={'路由算法验证'} />
        </CardTitle>
      </CardHeader>
      <CardContent className='space-y-3'>
        <Input
          value={model}
          placeholder={i18n.t('模型')}
          aria-label={i18n.t('模型')}
          onChange={(event) => onModelChange(event.target.value)}
        />
        <div className='route-insight-groups flex flex-wrap gap-2'>
          {groups.map((group) => (
            <button
              key={group.group}
              aria-pressed={selectedGroup === group.group}
              className={
                selectedGroup === group.group
                  ? 'bg-primary text-primary-foreground rounded-md px-3 py-1.5 text-sm'
                  : 'border-input hover:bg-accent rounded-md border px-3 py-1.5 text-sm'
              }
              onClick={() => onGroupChange(group.group)}
            >
              {group.group}
            </button>
          ))}
        </div>
        {!algorithmActive && selectedGroup && (
          <div className='route-insight-notice'>
            <Trans
              i18nKey={
                '当前分组仍使用旧优先级 / 权重路由。启用该分组的自动路由并保存后，才会使用成本、成功率、冷却和首字评分。'
              }
            />
          </div>
        )}
        {metricsLoading && <Skeleton className='h-20' />}
        {metrics?.members.map((member) => (
          <div
            key={member.channel_id}
            className='route-insight-member border-b py-2 last:border-0'
            data-eligible={member.eligible}
          >
            <div className='flex justify-between gap-2'>
              <span className='truncate font-medium'>
                {member.channel_name ||
                  i18n.t('渠道 #{{param0}}', { param0: member.channel_id })}
              </span>
              <span className='font-mono text-xs'>
                {member.score.toFixed(3)}
              </span>
            </div>
            <div className='text-muted-foreground mt-1 flex flex-wrap gap-x-2 gap-y-1 text-xs'>
              <span>
                {member.eligible ? i18n.t('候选可用') : i18n.t('当前排除')}
              </span>
              <span>{member.health.state || 'unknown'}</span>
              <span>{member.health.success_rate_5m?.toFixed(1) ?? '0.0'}%</span>
              <span>P95 {Math.round(member.health.ttft_p95_ms || 0)}ms</span>
            </div>
          </div>
        ))}
      </CardContent>
    </Card>
  )
}
