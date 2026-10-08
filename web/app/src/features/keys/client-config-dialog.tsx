import { useEffect } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from '../../lib/i18n'
import { Button, CopyButton, Dialog, Loading } from '../../components/ui'
import { clientConfigSnippets } from './client-config'
import { api, unwrap } from '../../lib/api'
import type { APIKey } from '../../lib/types'

/**
 * Reveals the key's secret on open (never cached outside this dialog's lifetime)
 * and renders ready-to-copy env/config snippets for common client tools.
 */
export function ClientConfigDialog(props: { apiKey: APIKey | null; onClose: () => void }) {
  const { t } = useTranslation()
  const reveal = useMutation({
    mutationFn: (id: APIKey['id']) =>
      api
        .POST('/api/token/{id}/key', { params: { path: { id: String(id) } } })
        .then((result) => unwrap(result)),
  })
  const open = props.apiKey !== null
  const keyID = props.apiKey?.id
  // Only re-fetch when a different key is opened; `reveal` is stable across renders.
  useEffect(() => {
    if (keyID !== undefined) reveal.mutate(keyID)
  }, [keyID])
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          reveal.reset()
          props.onClose()
        }
      }}
      title="客户端配置"
      description="复制下方配置到对应客户端或终端会话中使用。"
      size="lg"
    >
      {reveal.isPending && <Loading rows={3} />}
      {reveal.isError && <p className="error-message">{reveal.error.message}</p>}
      {reveal.data && (
        <div style={{ display: 'grid', gap: 'var(--space-4)' }}>
          {clientConfigSnippets(window.location.origin, reveal.data.key).map((snippet) => (
            <div key={snippet.id}>
              <p className="subtle" style={{ marginBottom: 'var(--space-2)' }}>
                {snippet.label}
              </p>
              <div className="code-block">
                <CopyButton value={snippet.code} label="复制配置" />
                <pre>
                  <code>{snippet.code}</code>
                </pre>
              </div>
            </div>
          ))}
        </div>
      )}
      <Button variant="quiet" onClick={props.onClose}>
        {t('关闭')}
      </Button>
    </Dialog>
  )
}
