// Batch toolbar for selected channel rows. Enable/disable/delete loop per
// channel because `/api/channel/*` has no bulk enable/disable endpoint
// (only POST /api/channel/batch for delete and POST /api/channel/batch/tag
// for tagging — see v3/internal/catalogcontrol/legacy_bulk.go). Partial
// failures are reported instead of silently stopping.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../../lib/api'
import { useTranslation } from '../../lib/i18n'
import { Button, Callout, confirmAction } from '../../components/ui'
import type { Channel } from './types'

async function settleEach<T>(ids: T[], run: (id: T) => Promise<unknown>): Promise<string[]> {
  const failures: string[] = []
  for (const id of ids) {
    try {
      await run(id)
    } catch (cause) {
      failures.push(`${String(id)}: ${cause instanceof Error ? cause.message : '请求失败'}`)
    }
  }
  return failures
}

export function ChannelBatchBar(props: { selected: Channel[]; onClear: () => void }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [tagValue, setTagValue] = useState('')
  const [failures, setFailures] = useState<string[]>([])
  const refresh = () => queryClient.invalidateQueries({ queryKey: ['channels'] })
  const setStatus = useMutation({
    mutationFn: async (status: 'enabled' | 'disabled') => {
      const result = await settleEach(props.selected, (channel) =>
        api.PUT('/api/catalog/channels/{id}', {
          params: { path: { id: String(channel.id) } },
          body: { ...channel, status },
        }),
      )
      setFailures(result)
      return result
    },
    onSuccess: () => void refresh(),
  })
  const remove = useMutation({
    mutationFn: () =>
      api.POST('/api/channel/batch', {
        body: { ids: props.selected.map((channel) => channel.id) },
      }),
    onSuccess: () => {
      setFailures([])
      props.onClear()
      void refresh()
    },
  })
  const setTag = useMutation({
    mutationFn: () =>
      api.POST('/api/channel/batch/tag', {
        body: { ids: props.selected.map((channel) => channel.id), tag: tagValue || null },
      }),
    onSuccess: () => {
      setFailures([])
      props.onClear()
      void refresh()
    },
  })
  if (props.selected.length === 0) return null
  return (
    <div className="filters section">
      <span className="subtle">
        {t('已选择')} {props.selected.length}
      </span>
      <Button
        variant="quiet"
        disabled={setStatus.isPending}
        onClick={() => setStatus.mutate('enabled')}
      >
        {t('启用')}
      </Button>
      <Button
        variant="quiet"
        disabled={setStatus.isPending}
        onClick={() => setStatus.mutate('disabled')}
      >
        {t('停用')}
      </Button>
      <label className="field" htmlFor="batch-tag">
        <span>{t('批量标签')}</span>
        <input
          id="batch-tag"
          value={tagValue}
          onChange={(event) => setTagValue(event.target.value)}
          placeholder={t('留空清除标签')}
        />
      </label>
      <Button variant="quiet" disabled={setTag.isPending} onClick={() => setTag.mutate()}>
        {t('设置标签')}
      </Button>
      <Button
        variant="danger"
        disabled={remove.isPending}
        onClick={async () => {
          if (
            await confirmAction({
              title: '批量删除渠道',
              description: '将删除所选渠道，此操作无法恢复。',
              danger: true,
            })
          )
            remove.mutate()
        }}
      >
        {t('批量删除')}
      </Button>
      <Button variant="ghost" onClick={props.onClear}>
        {t('取消选择')}
      </Button>
      {failures.length > 0 && (
        <Callout tone="warning" title="部分操作失败">
          {failures.map((message) => (
            <div key={message}>{message}</div>
          ))}
        </Callout>
      )}
    </div>
  )
}
