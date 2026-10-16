import { useState } from 'react'
import i18n from '@/i18n/config'
import { ChevronDown, ChevronRight, Save } from 'lucide-react'
import { useTranslation, Trans } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import type { RoutePoolGroup } from '../types'

type RoutePoolGroupListProps = {
  groups: RoutePoolGroup[]
  loading: boolean
  savingGroup?: string
  onSelectGroup: (group: string) => void
  onSave: (group: RoutePoolGroup) => void
}

export function RoutePoolGroupList({
  groups,
  loading,
  savingGroup,
  onSelectGroup,
  onSave,
}: RoutePoolGroupListProps) {
  if (loading) {
    return (
      <Card>
        <CardContent className='text-muted-foreground py-8 text-sm'>
          <Trans i18nKey={'正在加载已配置分组...'} />
        </CardContent>
      </Card>
    )
  }
  if (groups.length === 0) {
    return (
      <Card>
        <CardContent className='text-muted-foreground py-8 text-sm'>
          <Trans i18nKey={'尚未发现配置了分组的渠道。'} />
        </CardContent>
      </Card>
    )
  }
  return (
    <div className='route-pool-groups'>
      {groups.map((group) => (
        <RoutePoolGroupCard
          key={group.group}
          group={group}
          saving={savingGroup === group.group}
          onSelect={onSelectGroup}
          onSave={onSave}
        />
      ))}
    </div>
  )
}

function RoutePoolGroupCard({
  group,
  saving,
  onSelect,
  onSave,
}: {
  group: RoutePoolGroup
  saving: boolean
  onSelect: (group: string) => void
  onSave: (group: RoutePoolGroup) => void
}) {
  useTranslation()
  const [expanded, setExpanded] = useState(false)
  const [draft, setDraft] = useState(group)
  const [previousGroup, setPreviousGroup] = useState(group)
  if (previousGroup !== group) {
    setPreviousGroup(group)
    setDraft(group)
  }
  const enabledCount = draft.channels.filter(
    (channel) => channel.enabled
  ).length
  const updateChannel = (
    id: number,
    patch: Partial<RoutePoolGroup['channels'][number]>
  ) =>
    setDraft((current) => ({
      ...current,
      channels: current.channels.map((channel) =>
        channel.channel_id === id ? { ...channel, ...patch } : channel
      ),
    }))
  return (
    <Card className='route-pool-group' onClick={() => onSelect(draft.group)}>
      <CardHeader className='gap-3 py-4 sm:flex-row sm:items-center sm:justify-between'>
        <div className='min-w-0'>
          <CardTitle className='route-group-title flex items-center gap-2 text-base'>
            <button
              className='route-group-toggle text-muted-foreground hover:text-foreground'
              aria-expanded={expanded}
              aria-label={expanded ? i18n.t('收起分组') : i18n.t('展开分组')}
              onClick={() => setExpanded((value) => !value)}
            >
              {expanded ? (
                <ChevronDown className='size-4' />
              ) : (
                <ChevronRight className='size-4' />
              )}
            </button>
            <span className='route-group-name'>{draft.group}</span>
            <span
              className={
                draft.algorithm_active
                  ? 'route-algorithm-state is-active'
                  : 'route-algorithm-state'
              }
            >
              {draft.algorithm_active
                ? i18n.t('算法已接管，缓存粘性优先')
                : i18n.t('旧优先级 / 权重')}
            </span>
          </CardTitle>
          <dl className='mt-3 flex flex-wrap gap-6 text-sm'>
            <div className='flex gap-2'>
              <dt className='text-muted-foreground'>
                <Trans i18nKey={'渠道'} />
              </dt>
              <dd>{draft.channels.length}</dd>
            </div>
            <div className='flex gap-2'>
              <dt className='text-muted-foreground'>
                <Trans i18nKey={'参与自动路由'} />
              </dt>
              <dd>{enabledCount}</dd>
            </div>
            <div className='flex gap-2'>
              <dt className='text-muted-foreground'>
                <Trans i18nKey={'成员来源'} />
              </dt>
              <dd>
                {draft.auto_discover
                  ? i18n.t('自动同步')
                  : i18n.t('旧成员模式')}
              </dd>
            </div>
          </dl>
        </div>
        <div className='flex items-center gap-3 self-end sm:self-auto'>
          <span className='text-sm'>
            {draft.enabled ? i18n.t('启用算法') : i18n.t('不接管')}
          </span>
          <Switch
            aria-label={i18n.t('启用算法')}
            checked={draft.enabled}
            onCheckedChange={(enabled) =>
              setDraft((current) => ({
                ...current,
                enabled,
              }))
            }
          />
          <Button
            size='sm'
            onClick={() => onSave(draft)}
            disabled={
              saving ||
              draft.channels.some(
                (channel) =>
                  !Number.isFinite(channel.cost_multiplier) ||
                  channel.cost_multiplier <= 0
              )
            }
          >
            <Save />
            <Trans i18nKey={'保存'} />
          </Button>
        </div>
      </CardHeader>
      {expanded && (
        <CardContent className='border-t pt-0'>
          <div className='divide-y'>
            {draft.channels.map((channel) => (
              <div key={channel.channel_id} className='route-member-row'>
                <div className='min-w-0'>
                  <p className='truncate font-medium'>
                    <span className='font-mono'>#{channel.channel_id}</span>{' '}
                    {channel.channel_name}
                  </p>
                  <p className='text-muted-foreground truncate text-xs'>
                    {channel.models || i18n.t('未同步模型')}{' '}
                    {channel.channel_status === 1
                      ? ''
                      : i18n.t(' | 全局不可用')}
                  </p>
                </div>
                <div>
                  <p className='text-muted-foreground text-xs'>
                    <Trans i18nKey={'采购倍率'} />
                  </p>
                  <Input
                    type='number'
                    min='0.0001'
                    step='0.0001'
                    value={channel.cost_multiplier}
                    onChange={(event) =>
                      updateChannel(channel.channel_id, {
                        cost_multiplier: Number(event.target.value) || 1,
                      })
                    }
                  />
                </div>
                <div>
                  <p className='text-muted-foreground text-xs'>
                    <Trans i18nKey={'模型覆盖 JSON'} />
                  </p>
                  <Input
                    className='font-mono text-xs'
                    value={channel.model_cost_overrides}
                    onChange={(event) =>
                      updateChannel(channel.channel_id, {
                        model_cost_overrides: event.target.value,
                      })
                    }
                  />
                </div>
                <div>
                  <p className='text-muted-foreground text-xs'>
                    <Trans i18nKey={'故障域'} />
                  </p>
                  <Input
                    className='font-mono text-xs'
                    placeholder={i18n.t('自动按上游主机归类')}
                    value={channel.fault_domain}
                    onChange={(event) =>
                      updateChannel(channel.channel_id, {
                        fault_domain: event.target.value,
                      })
                    }
                  />
                </div>
                <div className='flex items-center gap-2'>
                  <Switch
                    aria-label={`${i18n.t('参与自动路由')} ${channel.channel_name}`}
                    checked={channel.enabled}
                    onCheckedChange={(enabled) =>
                      updateChannel(channel.channel_id, { enabled })
                    }
                  />
                  <span className='text-sm'>
                    {channel.enabled ? i18n.t('参与') : i18n.t('禁用')}
                  </span>
                </div>
              </div>
            ))}
          </div>
        </CardContent>
      )}
    </Card>
  )
}
