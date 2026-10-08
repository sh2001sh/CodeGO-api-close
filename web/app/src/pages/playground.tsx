import { useEffect, useRef, useState } from 'react'
import { useQuery, useSuspenseQuery } from '@tanstack/react-query'
import { Link, useSearch } from '@tanstack/react-router'
import { Code2, SlidersHorizontal } from 'lucide-react'
import { keysOptions } from '../lib/queries'
import { api, unwrap } from '../lib/api'
import { useTranslation } from '../lib/i18n'
import { Button, Callout, Dialog, ErrorMessage } from '../components/ui'
import { KeyPicker } from '../features/playground/key-picker'
import { SettingsPanel } from '../features/playground/settings-panel'
import { MessageThread } from '../features/playground/message-thread'
import { Composer } from '../features/playground/composer'
import { CodeDialog } from '../features/playground/code-dialog'
import { useModelOptions } from '../features/playground/use-model-options'
import { usePlayground } from '../features/playground/use-playground'
import { GatewayError } from '../features/playground/stream'

function guidanceFor(error: Error | null): string | null {
  if (!(error instanceof GatewayError)) return null
  if (error.status === 401) return '该 Key 无效或已被停用，请重新选择并使用一个有效的 Key。'
  if (error.status === 402) return '余额不足，请前往钱包充值后重试。'
  if (error.status === 403) return '所选分组或模型没有访问权限，请重新选择。'
  if (error.status === 429) return '请求过于频繁，请稍后再试。'
  return null
}

export default function PlaygroundPage() {
  const { t } = useTranslation()
  const requested = useSearch({ from: '/_authenticated/playground' })
  const appliedSelection = useRef('')
  const keys = useSuspenseQuery(keysOptions()).data ?? []
  const [selectedID, setSelectedID] = useState('')
  const [secret, setSecret] = useState<string | null>(null)
  const [codeOpen, setCodeOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const selectedKey = keys.find((key) => String(key.id) === selectedID) ?? null
  const playground = usePlayground(secret)
  const models = useModelOptions(selectedKey, secret, playground.settings.group)
  const groups = useQuery({
    queryKey: ['conversation-groups'],
    queryFn: ({ signal }) => api.GET('/api/user/self/groups', { signal }).then(unwrap),
  })
  const marketGroups = useQuery({
    queryKey: ['market-pool-group-options'],
    queryFn: ({ signal }) =>
      api.GET('/api/marketplace/route-pools/group-options', { signal }).then(unwrap),
  })
  const pools = useQuery({
    queryKey: ['market-pools'],
    queryFn: ({ signal }) => api.GET('/api/marketplace/route-pools', { signal }).then(unwrap),
  })
  const groupNames = Object.fromEntries([
    ...(marketGroups.data ?? []).map((group) => [
      group.routing_group,
      group.kind === 'market' ? `${group.name} · ${group.display_id}` : group.name,
    ]),
    ...(pools.data ?? [])
      .filter((pool) => pool.token_group)
      .map((pool) => [pool.token_group!, `${t('路由池')} · ${pool.name}`]),
  ])
  const requestedGroupUnavailable = Boolean(
    requested.group && groups.data && !groups.data.includes(requested.group),
  )
  useEffect(() => {
    if (!groups.data || !requested.group || requestedGroupUnavailable) return
    const selection = JSON.stringify([requested.group, requested.model])
    if (appliedSelection.current === selection) return
    appliedSelection.current = selection
    playground.setSettings((current) => ({
      ...current,
      group: requested.group,
      model: requested.model ?? '',
    }))
  }, [
    groups.data,
    requested.group,
    requested.model,
    requestedGroupUnavailable,
    playground.setSettings,
  ])
  const guidance = guidanceFor(playground.error)
  useEffect(() => {
    if (!playground.settings.model && models.models.length)
      playground.setSettings((current) => ({ ...current, model: models.models[0] }))
  }, [models.models, playground.settings.model, playground.setSettings])

  return (
    <section className="conversation-page" aria-label={t('对话')}>
      <header className="conversation-header">
        <h1>{t('对话')}</h1>
        <div className="actions">
          <Button variant="quiet" onClick={() => setSettingsOpen(true)}>
            <SlidersHorizontal size={16} aria-hidden />
            {t('对话设置')}
          </Button>
          <Button variant="quiet" onClick={() => setCodeOpen(true)}>
            <Code2 size={16} aria-hidden />
            {t('查看代码')}
          </Button>
        </div>
      </header>
      <div className="conversation-controls">
        <details className="conversation-key" open={!secret || undefined}>
          <summary>{selectedKey?.name ?? t('选择 API Key')}</summary>
          <KeyPicker
            keys={keys}
            selectedID={selectedID}
            onSelect={(id) => {
              setSelectedID(id)
              setSecret(null)
              playground.setSettings((current) => ({
                ...current,
                group: requestedGroupUnavailable ? '' : (requested.group ?? ''),
                model: requested.model ?? '',
              }))
            }}
            hasSecret={Boolean(secret)}
            disabled={playground.sending}
            onReveal={setSecret}
          />
        </details>
        <SettingsPanel
          settings={playground.settings}
          onChange={playground.setSettings}
          models={models.models}
          modelsPending={models.isPending}
          mode="model"
          disabled={playground.sending}
          groups={groups.data ?? []}
          groupNames={groupNames}
        />
      </div>
      <ErrorMessage error={models.error} />
      <ErrorMessage error={groups.error} />
      <ErrorMessage error={marketGroups.error} />
      {requestedGroupUnavailable && (
        <Callout tone="warning">
          {t('所选分组已不可用或没有访问权限，请返回市场重新选择。')}{' '}
          <Link to="/channel-market">{t('浏览市场')}</Link>
        </Callout>
      )}
      {secret && !models.isPending && !models.error && !models.models.length && (
        <div className="conversation-model-empty">
          <span>{t('该 Key 在所选分组暂无可用模型')}</span>
          <Link to="/keys">{t('管理 API Key')}</Link>
        </div>
      )}
      <div className="playground-main" data-empty={!playground.messages.length}>
        <div className="conversation-transcript">
          <MessageThread messages={playground.messages} />
          {playground.error && (
            <>
              <ErrorMessage error={playground.error} />
              {guidance && <Callout tone="warning">{t(guidance)}</Callout>}
            </>
          )}
        </div>
        <div className="conversation-input">
          {playground.usage && (
            <p className="subtle tabular">
              {t('用量')} · {t('输入 token')} {playground.usage.prompt_tokens} · {t('输出 token')}{' '}
              {playground.usage.completion_tokens} · {t('总计')} {playground.usage.total_tokens}
            </p>
          )}
          <Composer
            sending={playground.sending}
            disabled={!secret || !playground.settings.model || requestedGroupUnavailable}
            canRegenerate={playground.messages.some((message) => message.role === 'user')}
            hasMessages={playground.messages.length > 0}
            onSend={playground.send}
            onStop={playground.stop}
            onRegenerate={playground.regenerate}
            onClear={playground.clear}
          />
        </div>
      </div>
      <Dialog
        open={settingsOpen}
        onOpenChange={setSettingsOpen}
        title="对话设置"
        footer={
          <Button variant="secondary" onClick={() => setSettingsOpen(false)}>
            {t('关闭')}
          </Button>
        }
      >
        <SettingsPanel
          settings={playground.settings}
          onChange={playground.setSettings}
          models={models.models}
          modelsPending={models.isPending}
          mode="advanced"
          disabled={playground.sending}
        />
      </Dialog>
      <CodeDialog
        open={codeOpen}
        onOpenChange={setCodeOpen}
        history={playground.messages}
        settings={playground.settings}
      />
    </section>
  )
}
