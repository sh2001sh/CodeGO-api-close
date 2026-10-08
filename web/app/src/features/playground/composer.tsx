import { useRef } from 'react'
import { RotateCcw, Send, Trash2, Square } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { Button } from '../../components/ui'

export function Composer(props: {
  sending: boolean
  disabled?: boolean
  canRegenerate: boolean
  hasMessages: boolean
  onSend: (content: string) => boolean | void
  onStop: () => void
  onRegenerate: () => void
  onClear: () => void
}) {
  const { t } = useTranslation()
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const submit = () => {
    if (props.disabled || props.sending) return
    const value = inputRef.current?.value.trim()
    if (!value) return
    if (props.onSend(value) === false) return
    if (inputRef.current) inputRef.current.value = ''
  }
  return (
    <div className="playground-composer">
      <textarea
        ref={inputRef}
        rows={3}
        disabled={props.disabled}
        aria-label={t('消息输入')}
        placeholder={t('输入消息，按 Enter 发送，Shift+Enter 换行')}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
            event.preventDefault()
            submit()
          }
        }}
      />
      <div className="playground-composer-actions">
        <Button
          variant="quiet"
          size="sm"
          disabled={!props.hasMessages || props.sending}
          onClick={props.onClear}
        >
          <Trash2 size={14} aria-hidden />
          {t('清空')}
        </Button>
        <Button
          variant="quiet"
          size="sm"
          disabled={!props.canRegenerate || props.sending}
          onClick={props.onRegenerate}
        >
          <RotateCcw size={14} aria-hidden />
          {t('重新生成')}
        </Button>
        {props.sending ? (
          <Button variant="danger" size="sm" onClick={props.onStop}>
            <Square size={14} aria-hidden />
            {t('停止')}
          </Button>
        ) : (
          <Button size="sm" disabled={props.disabled} onClick={submit}>
            <Send size={14} aria-hidden />
            {t('发送')}
          </Button>
        )}
      </div>
    </div>
  )
}
