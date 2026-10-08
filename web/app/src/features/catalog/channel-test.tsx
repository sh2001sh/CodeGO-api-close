// Per-row and bulk channel connectivity test ("测速"). Uses the legacy
// /api/channel/test(/{id}) endpoints — the only probe endpoints available;
// there is no /api/catalog/channels/{id}/test equivalent. Unlike most other
// handlers, these two respond with the probe result/batch directly (no
// {success, data} envelope — see legacyTestChannel/legacyTestChannels in
// v3/internal/catalogcontrol/legacy_probe.go), so unwrap() does not apply.
import { useMutation } from '@tanstack/react-query'
import { APIError, api } from '../../lib/api'
import { useTranslation } from '../../lib/i18n'
import { Button, Badge } from '../../components/ui'
import type { Channel, ProbeBatch, ProbeResult } from './types'

export function useChannelTest() {
  return useMutation({
    mutationFn: async (id: Channel['id']) => {
      const result = await api.GET('/api/channel/test/{id}', {
        params: { path: { id: String(id) } },
      })
      if (!result.data) throw new APIError('服务器返回了无效响应', result.response.status)
      return result.data as unknown as ProbeResult
    },
  })
}

export function useChannelTestAll() {
  return useMutation({
    mutationFn: async () => {
      const result = await api.GET('/api/channel/test')
      if (!result.data) throw new APIError('服务器返回了无效响应', result.response.status)
      return result.data as unknown as ProbeBatch
    },
  })
}

export function ChannelTestCell(props: {
  result?: ProbeResult
  pending: boolean
  onTest: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="row-actions">
      <Button variant="quiet" size="sm" disabled={props.pending} onClick={props.onTest}>
        {t('测速')}
      </Button>
      {props.result &&
        (props.result.success ? (
          <Badge tone="success">{Math.round(props.result.time * 1000)}ms</Badge>
        ) : (
          <Badge tone="danger">{t(props.result.message)}</Badge>
        ))}
    </div>
  )
}
