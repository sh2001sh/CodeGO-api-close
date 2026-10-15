import { useEffect, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { ArrowUpRight, Settings2 } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { Button, CopyButton, Dialog, ErrorMessage, Field, SelectField } from '../../components/ui'
import { ccSwitchImportURL, clientTools } from './client-config'
import { api, unwrap } from '../../lib/api'
import type { APIKey, Schema } from '../../lib/types'

export function ClientConfigDialog(props: { apiKey: APIKey | null; onClose: () => void }) {
  const { t } = useTranslation()
  const [tool, setTool] = useState('codex')
  const [linkError, setLinkError] = useState<Error | null>(null)
  const createImport = useMutation({
    gcTime: 0,
    mutationFn: (body: Schema['DesktopImportInput']) =>
      api.POST('/api/desktop/import/deeplink', { body }).then(unwrap),
  })
  const keyID = props.apiKey?.id
  useEffect(() => {
    createImport.reset()
    setLinkError(null)
  }, [keyID])
  const close = () => {
    createImport.reset()
    setLinkError(null)
    props.onClose()
  }
  return (
    <Dialog
      open={props.apiKey !== null}
      onOpenChange={(open) => !open && close()}
      title="CC Switch"
      size="lg"
    >
      <form
        key={String(keyID)}
        className="form-stack"
        onChange={() => {
          createImport.reset()
          setLinkError(null)
        }}
        onSubmit={(event) => {
          event.preventDefault()
          if (!props.apiKey) return
          const form = new FormData(event.currentTarget)
          createImport.mutate({
            target: 'ccswitch',
            token_id: props.apiKey.id,
            tool,
            name: String(form.get('name') ?? '').trim(),
            model: String(form.get('model') ?? '').trim(),
            haiku_model: String(form.get('haiku_model') ?? '').trim(),
            sonnet_model: String(form.get('sonnet_model') ?? '').trim(),
            opus_model: String(form.get('opus_model') ?? '').trim(),
            enabled: true,
          })
        }}
      >
        <div className="field">
          <label className="field-label" htmlFor="ccswitch-tool">
            {t('工具')}
          </label>
          <select id="ccswitch-tool" value={tool} onChange={(event) => setTool(event.target.value)}>
            {clientTools.map((item) => (
              <option key={item.value} value={item.value}>
                {item.label}
              </option>
            ))}
          </select>
        </div>
        <Field
          name="name"
          label="配置名称"
          required
          defaultValue={props.apiKey?.name ?? 'CodeGo'}
          maxLength={100}
        />
        {props.apiKey?.allowed_models?.length ? (
          <SelectField
            name="model"
            label="模型"
            required
            options={props.apiKey.allowed_models.map((model) => ({ value: model, label: model }))}
          />
        ) : (
          <Field name="model" label="模型" required maxLength={256} />
        )}
        {tool === 'claude' && (
          <div className="form-panel">
            <Field name="haiku_model" label="Haiku" maxLength={256} />
            <Field name="sonnet_model" label="Sonnet" maxLength={256} />
            <Field name="opus_model" label="Opus" maxLength={256} />
          </div>
        )}
        <ErrorMessage error={createImport.error ?? linkError} />
        <div className="row-actions">
          <Button
            type="submit"
            disabled={!props.apiKey || createImport.isPending}
            loading={createImport.isPending}
          >
            <Settings2 size={16} aria-hidden />
            {t('生成配置')}
          </Button>
          {createImport.data && (
            <>
              <Button
                onClick={() => {
                  try {
                    window.location.assign(ccSwitchImportURL(createImport.data.deep_link))
                  } catch (cause) {
                    setLinkError(cause instanceof Error ? cause : new Error('导入链接无效'))
                  }
                }}
              >
                <ArrowUpRight size={16} aria-hidden />
                {t('导入 CC Switch')}
              </Button>
              <CopyButton value={createImport.data.deep_link} label="复制配置" />
            </>
          )}
          <Button variant="quiet" onClick={close}>
            {t('关闭')}
          </Button>
        </div>
      </form>
    </Dialog>
  )
}
