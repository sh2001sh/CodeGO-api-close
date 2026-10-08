import { Bot, Copy, User } from 'lucide-react'
import { useTranslation } from '../../lib/i18n'
import { useToast } from '../../hooks/use-toast'
import { IconButton } from '../../components/ui'
import { MarkdownLite } from './markdown-lite'
import type { ChatMessage } from './types'

function MessageBubble(props: { message: ChatMessage }) {
  const { t } = useTranslation()
  const toast = useToast((state) => state.add)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(props.message.content)
    } catch {
      toast(t('复制失败，请手动复制'), 'error')
    }
  }
  return (
    <div className="chat-message" data-role={props.message.role}>
      <span className="chat-avatar" aria-hidden>
        {props.message.role === 'user' ? <User size={16} /> : <Bot size={16} />}
      </span>
      <div className="chat-bubble">
        {props.message.reasoning && (
          <details className="chat-reasoning">
            <summary>{t('推理过程')}</summary>
            <p>{props.message.reasoning}</p>
          </details>
        )}
        {props.message.status === 'streaming' && !props.message.content ? (
          <p className="chat-text muted">{t('正在生成…')}</p>
        ) : (
          <MarkdownLite content={props.message.content} />
        )}
        {props.message.status === 'error' && (
          <p className="chat-error" role="alert">
            {props.message.errorMessage}
          </p>
        )}
        {props.message.content && (
          <div className="chat-actions">
            <IconButton label={t('复制')} onClick={() => void copy()}>
              <Copy size={13} aria-hidden />
            </IconButton>
          </div>
        )}
      </div>
    </div>
  )
}

export function MessageThread(props: { messages: ChatMessage[] }) {
  const { t } = useTranslation()
  if (!props.messages.length)
    return (
      <div className="conversation-empty">
        <img src="/brand/codego-logo.svg" alt="" width="44" height="44" />
        <h2>{t('今天想聊些什么？')}</h2>
      </div>
    )
  return (
    <div className="chat-thread" role="log" aria-live="polite">
      {props.messages.map((message) => (
        <MessageBubble key={message.id} message={message} />
      ))}
    </div>
  )
}
