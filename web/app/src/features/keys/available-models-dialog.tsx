import { Boxes } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { Button, Dialog } from '../../components/ui'
import type { APIKey } from '../../lib/types'

export function AvailableModelsDialog(props: { apiKey: APIKey | null; onClose: () => void }) {
  const { t } = useTranslation()
  const models = props.apiKey?.allowed_models ?? []
  return (
    <Dialog
      open={props.apiKey !== null}
      onOpenChange={(open) => !open && props.onClose()}
      title="可用模型"
    >
      {models.length ? (
        <ul style={{ display: 'grid', gap: 6, padding: 0, margin: 0, listStyle: 'none' }}>
          {models.map((model) => (
            <li key={model} className="mono badge" style={{ justifySelf: 'start' }}>
              {model}
            </li>
          ))}
        </ul>
      ) : (
        <p className="muted">
          <Boxes size={14} aria-hidden /> {t('未设置限制，可调用全部模型')}
        </p>
      )}
      <Button variant="quiet" onClick={props.onClose}>
        {t('关闭')}
      </Button>
    </Dialog>
  )
}
