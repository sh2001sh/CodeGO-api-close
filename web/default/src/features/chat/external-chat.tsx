import { useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import {
  ArrowLeft,
  ArrowUpRight,
  Loader2,
  MessageCircleWarning,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useStatus } from '@/hooks/use-status'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import './external-chat.css'
import { useActiveChatKey } from './hooks/use-active-chat-key'
import { useChatPresets } from './hooks/use-chat-presets'
import { chatLinkRequiresApiKey, resolveChatUrl } from './lib/chat-links'

export function ExternalChat(props: { chatId: string }) {
  const { t } = useTranslation()
  const { loading: statusLoading, error: statusError } = useStatus()
  const { chatPresets, serverAddress } = useChatPresets()
  const preset = chatPresets[Number(props.chatId)]
  const isWeb = preset?.type === 'web'
  const requiresKey = Boolean(
    isWeb && preset && chatLinkRequiresApiKey(preset.url)
  )
  const keyQuery = useActiveChatKey(Boolean(preset && requiresKey))
  const target = useMemo(() => {
    if (!isWeb || !preset) return ''
    try {
      const url = new URL(preset.url)
      return ['http:', 'https:'].includes(url.protocol) ? url.origin : ''
    } catch {
      return ''
    }
  }, [isWeb, preset])
  const iframeSrc = useMemo(() => {
    if (!preset || !isWeb || !target || (requiresKey && !keyQuery.data))
      return ''
    const url = resolveChatUrl({
      template: preset.url,
      apiKey: requiresKey ? keyQuery.data : undefined,
      serverAddress,
    })
    try {
      return new URL(url).origin === target ? url : ''
    } catch {
      return ''
    }
  }, [isWeb, keyQuery.data, preset, requiresKey, serverAddress, target])
  const preparing = statusLoading || (requiresKey && keyQuery.isPending)
  return (
    <div className='external-chat-page'>
      <header className='external-chat-heading'>
        <div>
          <Button
            variant='ghost'
            size='icon'
            aria-label={t('Return to dashboard')}
            render={<Link to='/dashboard' />}
          >
            <ArrowLeft aria-hidden='true' />
          </Button>
          <h1>{preset?.name ?? t('Chat')}</h1>
        </div>
        {target && <span>{target}</span>}
        {iframeSrc && (
          <Button
            variant='outline'
            onClick={() =>
              window.open(iframeSrc, '_blank', 'noopener,noreferrer')
            }
          >
            {t('打开')}
            <ArrowUpRight aria-hidden='true' />
          </Button>
        )}
      </header>
      {preparing ? (
        <div className='external-chat-state' role='status'>
          <Loader2 className='animate-spin' aria-hidden='true' />
          <span>{t('Preparing your chat link…')}</span>
        </div>
      ) : !preset ? (
        <div className='external-chat-state'>
          <MessageCircleWarning aria-hidden='true' />
          <h2>{t('Chat preset not found')}</h2>
          {statusError && (
            <Alert variant='destructive'>
              <AlertTitle>{t('Unable to open chat')}</AlertTitle>
            </Alert>
          )}
          <Button variant='outline' render={<Link to='/dashboard' />}>
            {t('Return to dashboard')}
          </Button>
        </div>
      ) : !isWeb ? (
        <div className='external-chat-state'>
          <MessageCircleWarning aria-hidden='true' />
          <h2>{t('Use sidebar shortcut')}</h2>
          <Button variant='outline' render={<Link to='/keys' />}>
            {t('API Keys')}
          </Button>
        </div>
      ) : !iframeSrc || keyQuery.isError ? (
        <div className='external-chat-state'>
          <Alert variant='destructive'>
            <AlertTitle>{t('Unable to open chat')}</AlertTitle>
            <AlertDescription>
              {keyQuery.error instanceof Error
                ? t(keyQuery.error.message)
                : t(
                    'Unable to generate chat link. Please check your API keys.'
                  )}
            </AlertDescription>
          </Alert>
          <div className='flex flex-wrap gap-3'>
            {requiresKey && (
              <Button
                variant='outline'
                onClick={() => void keyQuery.refetch()}
                disabled={keyQuery.isFetching}
              >
                {t('Retry')}
              </Button>
            )}
            <Button render={<Link to='/keys' />}>{t('API Keys')}</Button>
            <Button variant='outline' render={<Link to='/dashboard' />}>
              {t('Return to dashboard')}
            </Button>
          </div>
        </div>
      ) : (
        <iframe
          src={iframeSrc}
          key={iframeSrc}
          allow='camera; microphone'
          referrerPolicy='no-referrer'
          title={`${t('Chat')}: ${preset.name}`}
        />
      )}
    </div>
  )
}
