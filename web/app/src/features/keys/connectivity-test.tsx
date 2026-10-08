import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Activity } from 'lucide-react'
import { api, unwrap } from '../../lib/api'
import { useTranslation } from '../../lib/i18n'
import { Button, Callout, Dialog } from '../../components/ui'
import type { APIKey } from '../../lib/types'

/**
 * Sends a tiny real chat completion through the gateway using the key's own secret,
 * then reports latency or the upstream error. The secret is fetched on demand and
 * discarded once the test completes; it is never persisted.
 */
export function ConnectivityTestDialog(props: { apiKey: APIKey | null; onClose: () => void }) {
  const { t } = useTranslation()
  const [model, setModel] = useState('')
  const test = useMutation({
    mutationFn: async (id: APIKey['id']) => {
      const secret = await api
        .POST('/api/token/{id}/key', { params: { path: { id: String(id) } } })
        .then((result) => unwrap(result))
      const started = performance.now()
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${secret.key}` },
        body: JSON.stringify({
          model: model || props.apiKey?.allowed_models?.[0] || 'gpt-4o-mini',
          messages: [{ role: 'user', content: 'ping' }],
          max_tokens: 1,
          stream: false,
        }),
      })
      const latency = Math.round(performance.now() - started)
      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as {
          error?: { message?: string }
        } | null
        throw new Error(body?.error?.message || `HTTP ${response.status}`)
      }
      return latency
    },
  })
  return (
    <Dialog
      open={props.apiKey !== null}
      onOpenChange={(open) => {
        if (!open) {
          test.reset()
          props.onClose()
        }
      }}
      title="连通性测试"
      description="使用该 Key 向网关发起一次真实的最小请求，并按正常调用计费。"
    >
      <div className="field">
        <label className="field-label" htmlFor="connectivity-model">
          {t('测试模型')}
        </label>
        <input
          id="connectivity-model"
          value={model}
          onChange={(event) => setModel(event.target.value)}
          placeholder={props.apiKey?.allowed_models?.[0] || 'gpt-4o-mini'}
        />
      </div>
      {test.isSuccess && (
        <Callout tone="success" title={t('连通正常')}>
          {t('延迟')} {test.data} ms
        </Callout>
      )}
      {test.isError && (
        <Callout tone="danger" title={t('测试失败')}>
          {test.error.message}
        </Callout>
      )}
      <div className="row-actions">
        <Button
          disabled={!props.apiKey || test.isPending}
          loading={test.isPending}
          onClick={() => props.apiKey && test.mutate(props.apiKey.id)}
        >
          <Activity size={14} aria-hidden />
          {t('开始测试')}
        </Button>
        <Button variant="quiet" onClick={props.onClose}>
          {t('关闭')}
        </Button>
      </div>
    </Dialog>
  )
}
