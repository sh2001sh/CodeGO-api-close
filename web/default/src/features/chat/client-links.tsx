import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowUpRight, Copy, KeyRound, Link2, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { Alert, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { fetchTokenKey, getApiKeys } from '@/features/keys/api'
import { API_KEY_STATUS } from '@/features/keys/constants'
import { normalizeFullApiKey } from '@/features/keys/lib/normalize-full-api-key'
import './client-links.css'
import { useChatPresets } from './hooks/use-chat-presets'
import { chatLinkRequiresApiKey, resolveChatUrl } from './lib/chat-links'

export function ClientLinks() {
  const { t } = useTranslation()
  const { chatPresets, serverAddress } = useChatPresets()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const presets = useMemo(
    () => chatPresets.filter((preset) => preset.type === 'web'),
    [chatPresets]
  )
  const [presetId, setPresetId] = useState('')
  const [keyId, setKeyId] = useState('')
  const [prepared, setPrepared] = useState<{
    url: string
    preview: string
  } | null>(null)
  const [generating, setGenerating] = useState(false)
  const [generationError, setGenerationError] = useState('')
  const preset = presets.find((item) => item.id === presetId) ?? presets[0]
  const requiresKey = Boolean(preset && chatLinkRequiresApiKey(preset.url))
  const keysQuery = useQuery({
    queryKey: ['client-link-keys', userId],
    enabled: requiresKey,
    queryFn: async () => {
      const result = await getApiKeys({ p: 1, size: 100 })
      if (!result.success)
        throw new Error(result.message || 'Failed to load API keys')
      return (result.data?.items ?? []).filter(
        (key) => key.status === API_KEY_STATUS.ENABLED
      )
    },
  })
  const keys = keysQuery.data ?? []
  const selectedKey = keys.find((key) => String(key.id) === keyId) ?? keys[0]
  let target = '—'
  try {
    if (preset) target = new URL(preset.url).origin
  } catch {
    target = '—'
  }
  const resetResult = () => {
    setPrepared(null)
    setGenerationError('')
  }
  const generate = async () => {
    if (!preset || (requiresKey && !selectedKey)) return
    setGenerating(true)
    setGenerationError('')
    try {
      let apiKey = ''
      if (requiresKey && selectedKey) {
        const response = await fetchTokenKey(selectedKey.id)
        if (!response.success || !response.data?.key)
          throw new Error(response.message || t('Failed to load API key'))
        apiKey = normalizeFullApiKey(response.data.key)
      }
      const url = resolveChatUrl({
        template: preset.url,
        apiKey,
        serverAddress,
      })
      const parsed = new URL(url)
      if (
        !['http:', 'https:'].includes(parsed.protocol) ||
        parsed.origin !== target
      )
        throw new Error(t('Unable to open chat'))
      const preview = resolveChatUrl({
        template: preset.url,
        apiKey: requiresKey ? '••••••••' : undefined,
        serverAddress,
      })
      setPrepared({ url, preview })
    } catch (error) {
      setGenerationError(
        error instanceof Error ? error.message : t('Unable to open chat')
      )
    } finally {
      setGenerating(false)
    }
  }
  const copy = async () => {
    if (!prepared) return
    if (await copyToClipboard(prepared.url)) toast.success(t('已复制'))
    else toast.error(t('Copy failed'))
  }
  return (
    <div className='client-link-page'>
      <header>
        <div className='client-link-title'>
          <span className='client-link-title-icon' aria-hidden='true'>
            <Link2 />
          </span>
          <h1>{t('客户端链接')}</h1>
        </div>
      </header>
      <div className='client-link-layout'>
        <section className='client-link-form'>
          {presets.length === 0 ? (
            <div className='client-link-empty' role='status'>
              <Link2 aria-hidden='true' />
              <h2>{t('No available Web chat links')}</h2>
              <Button variant='outline' render={<Link to='/dashboard' />}>
                {t('Return to dashboard')}
              </Button>
            </div>
          ) : (
            <>
              <div className='client-link-field'>
                <Label htmlFor='client-link-preset'>{t('客户端')}</Label>
                <Select
                  value={preset?.id ?? ''}
                  items={presets.map((item) => ({
                    value: item.id,
                    label: item.name,
                  }))}
                  onValueChange={(value) => {
                    setPresetId(value ?? '')
                    resetResult()
                  }}
                  disabled={generating}
                >
                  <SelectTrigger id='client-link-preset' className='w-full'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {presets.map((item) => (
                      <SelectItem key={item.id} value={item.id}>
                        {item.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {requiresKey && (
                <div className='client-link-field'>
                  <Label htmlFor='client-link-key'>{t('API Key')}</Label>
                  {keysQuery.isError ? (
                    <Alert variant='destructive'>
                      <AlertTitle>{t(keysQuery.error.message)}</AlertTitle>
                      <Button
                        variant='outline'
                        onClick={() => void keysQuery.refetch()}
                      >
                        {t('Retry')}
                      </Button>
                    </Alert>
                  ) : null}
                  <Select
                    value={selectedKey ? String(selectedKey.id) : ''}
                    items={keys.map((key) => ({
                      value: String(key.id),
                      label: key.name,
                    }))}
                    onValueChange={(value) => {
                      setKeyId(value ?? '')
                      resetResult()
                    }}
                    disabled={generating || keysQuery.isPending || !keys.length}
                  >
                    <SelectTrigger id='client-link-key' className='w-full'>
                      <SelectValue
                        placeholder={
                          keysQuery.isPending
                            ? t('Loading...')
                            : t('No enabled tokens available')
                        }
                      />
                    </SelectTrigger>
                    <SelectContent>
                      {keys.map((key) => (
                        <SelectItem key={key.id} value={String(key.id)}>
                          {key.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {selectedKey && (
                    <code className='client-key-mask'>{`${selectedKey.key.slice(0, 6)}••••${selectedKey.key.slice(-4)}`}</code>
                  )}
                  {!keysQuery.isPending && !keys.length && (
                    <Button variant='outline' render={<Link to='/keys' />}>
                      <KeyRound size={18} aria-hidden='true' />
                      {t('API Keys')}
                      <ArrowUpRight size={18} aria-hidden='true' />
                    </Button>
                  )}
                </div>
              )}
              {generationError && (
                <Alert variant='destructive'>
                  <AlertTitle>{generationError}</AlertTitle>
                </Alert>
              )}
              <Button
                className='client-link-generate'
                size='lg'
                disabled={
                  generating ||
                  (requiresKey &&
                    (!selectedKey || keysQuery.isPending || keysQuery.isError))
                }
                onClick={() => void generate()}
              >
                {generating ? (
                  <Loader2 className='animate-spin' aria-hidden='true' />
                ) : (
                  <Link2 aria-hidden='true' />
                )}
                {t('生成链接')}
              </Button>
              {prepared && (
                <div className='client-link-result' role='status'>
                  <code>{prepared.preview}</code>
                  <div>
                    <Button variant='outline' onClick={() => void copy()}>
                      <Copy aria-hidden='true' />
                      {t('复制')}
                    </Button>
                    <Button
                      onClick={() =>
                        window.open(
                          prepared.url,
                          '_blank',
                          'noopener,noreferrer'
                        )
                      }
                    >
                      {t('打开')}
                      <ArrowUpRight aria-hidden='true' />
                    </Button>
                  </div>
                </div>
              )}
            </>
          )}
        </section>
        <aside className='client-link-target'>
          <h2>{t('目标')}</h2>
          <dl>
            <div>
              <dt>{t('客户端')}</dt>
              <dd>{preset?.name ?? '—'}</dd>
            </div>
            <div>
              <dt>{t('域名')}</dt>
              <dd>{target}</dd>
            </div>
            <div>
              <dt>{t('Base URL')}</dt>
              <dd>
                <code>{serverAddress}</code>
              </dd>
            </div>
          </dl>
        </aside>
      </div>
    </div>
  )
}
