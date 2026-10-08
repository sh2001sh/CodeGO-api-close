import { useState } from 'react'
import { Button, ErrorMessage, Field } from './ui'
import { useTranslation } from '../lib/i18n'
import { FetchModelsAction } from '../features/catalog/fetch-models'
import type { Channel } from '../features/catalog/types'

export type { Channel }

function mapping(value: unknown): Record<string, string> | null {
  if (value === null) return null
  if (value === undefined) return {}
  if (typeof value !== 'object' || Array.isArray(value)) throw new Error('映射配置必须为对象')
  const result: Record<string, string> = {}
  for (const [key, item] of Object.entries(value)) {
    if (typeof item !== 'string') throw new Error('映射配置值必须为字符串')
    result[key] = item
  }
  return result
}

function advancedConfiguration(
  source: string,
): Pick<
  Channel,
  'model_mapping' | 'param_override' | 'header_override' | 'status_code_mapping' | 'settings'
> {
  const value: unknown = JSON.parse(source || '{}')
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('高级配置必须为对象')
  const data = Object.fromEntries(Object.entries(value))
  return {
    model_mapping: mapping(data.model_mapping),
    header_override: mapping(data.header_override),
    param_override: data.param_override ?? {},
    status_code_mapping: data.status_code_mapping ?? {},
    settings: data.settings ?? {},
  }
}

export function ChannelForm(props: {
  channel?: Channel
  pending: boolean
  onSave: (channel: Channel) => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const [error, setError] = useState<Error | null>(null)
  const c = props.channel
  const [provider, setProvider] = useState(c?.provider ?? 'openai')
  const [baseURL, setBaseURL] = useState(c?.base_url ?? '')
  const [secret, setSecret] = useState('')
  const [models, setModels] = useState(c?.models ?? [])
  const submit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setError(null)
    const form = new FormData(event.currentTarget)
    const text = (key: string) => String(form.get(key) ?? '')
    const list = (key: string) =>
      text(key)
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean)
    try {
      const advanced = advancedConfiguration(text('advanced'))
      const credentials = secret.trim()
        ? {
            credentials: [{ secret, kind: text('credential_kind') }],
            append_credentials: Boolean(c),
          }
        : {}
      props.onSave({
        ...c,
        id: c?.id ?? 0,
        tag: text('tag') || null,
        name: text('name'),
        provider,
        base_url: baseURL,
        proxy_url: text('proxy_url'),
        status: text('status'),
        scope: text('scope'),
        owner_user_id: text('owner_user_id') ? Number(text('owner_user_id')) : null,
        priority: Number(text('priority')),
        weight: Number(text('weight')),
        max_concurrency: Number(text('max_concurrency')),
        max_user_concurrency: Number(text('max_user_concurrency')),
        groups: list('groups'),
        models,
        remark: text('remark'),
        auto_disable: form.has('auto_disable'),
        multiplier_card_supported: form.has('multiplier_card_supported'),
        ...advanced,
        ...credentials,
      })
    } catch (failure) {
      setError(failure instanceof Error ? failure : new Error('高级配置必须为有效 JSON'))
    }
  }
  return (
    <form className="form-panel" onSubmit={submit}>
      <Field name="name" label="名称" required defaultValue={c?.name} maxLength={255} />
      <label className="field" htmlFor="provider">
        <span>{t('Provider')}</span>
        <input
          id="provider"
          name="provider"
          required
          value={provider}
          onChange={(event) => setProvider(event.target.value)}
        />
      </label>
      <label className="field" htmlFor="base_url">
        <span>{t('上游地址')}</span>
        <input
          id="base_url"
          name="base_url"
          type="url"
          value={baseURL}
          onChange={(event) => setBaseURL(event.target.value)}
        />
      </label>
      <Field name="proxy_url" label="代理地址" defaultValue={c?.proxy_url} />
      <Field
        name="groups"
        label="分组"
        defaultValue={c?.groups?.join(', ') ?? 'default'}
        required
      />
      <Field name="tag" label="标签" defaultValue={c?.tag ?? ''} />
      <label className="field" htmlFor="models_display">
        <span>{t('模型')}</span>
        <input
          id="models_display"
          required
          value={models.join(', ')}
          onChange={(event) =>
            setModels(
              event.target.value
                .split(',')
                .map((item) => item.trim())
                .filter(Boolean),
            )
          }
        />
      </label>
      <FetchModelsAction
        channelID={c ? String(c.id) : undefined}
        provider={provider}
        baseURL={baseURL}
        secret={secret}
        selected={models}
        onPick={setModels}
      />
      <Field name="priority" label="优先级" type="number" defaultValue={String(c?.priority ?? 0)} />
      <Field
        name="weight"
        label="权重"
        type="number"
        min={0}
        defaultValue={String(c?.weight ?? 1)}
      />
      <Field
        name="max_concurrency"
        label="渠道并发"
        type="number"
        min={0}
        defaultValue={String(c?.max_concurrency ?? 0)}
      />
      <Field
        name="max_user_concurrency"
        label="用户并发"
        type="number"
        min={0}
        defaultValue={String(c?.max_user_concurrency ?? 0)}
      />
      <label className="field" htmlFor="channel-status">
        <span>{t('状态')}</span>
        <select id="channel-status" name="status" defaultValue={c?.status ?? 'enabled'}>
          <option value="enabled">{t('enabled')}</option>
          <option value="disabled">{t('disabled')}</option>
          <option value="auto_disabled">{t('auto_disabled')}</option>
        </select>
      </label>
      <label className="field" htmlFor="channel-scope">
        <span>{t('渠道范围')}</span>
        <select id="channel-scope" name="scope" defaultValue={c?.scope ?? 'official'}>
          <option value="official">{t('官方')}</option>
          <option value="marketplace">{t('市场')}</option>
        </select>
      </label>
      <Field
        name="owner_user_id"
        label="渠道主用户 ID"
        type="number"
        min={1}
        defaultValue={String(c?.owner_user_id ?? '')}
      />
      <Field name="remark" label="备注" defaultValue={c?.remark} />
      <label className="field" htmlFor="secret">
        <span>{t(c ? '添加凭据' : '凭据')}</span>
        <input
          id="secret"
          type="password"
          value={secret}
          onChange={(event) => setSecret(event.target.value)}
        />
      </label>
      <label className="field" htmlFor="credential-kind">
        <span>{t('凭据类型')}</span>
        <select id="credential-kind" name="credential_kind">
          <option value="api_key">API Key</option>
          <option value="oauth">OAuth</option>
        </select>
      </label>
      <label className="checkbox-field">
        <input type="checkbox" name="auto_disable" defaultChecked={c?.auto_disable} />
        {t('自动停用')}
      </label>
      <label className="checkbox-field">
        <input
          type="checkbox"
          name="multiplier_card_supported"
          defaultChecked={c?.multiplier_card_supported}
        />
        {t('支持倍率卡')}
      </label>
      <label className="field full-width" style={{ flexBasis: '100%' }} htmlFor="advanced">
        <span>{t('高级配置')}</span>
        <textarea
          rows={6}
          id="advanced"
          name="advanced"
          defaultValue={JSON.stringify(
            {
              model_mapping: c?.model_mapping ?? {},
              param_override: c?.param_override ?? {},
              header_override: c?.header_override ?? {},
              status_code_mapping: c?.status_code_mapping ?? {},
              settings: c?.settings ?? {},
            },
            null,
            2,
          )}
        />
      </label>
      <ErrorMessage error={error} />
      <Button type="submit" disabled={props.pending}>
        {t('保存')}
      </Button>
      <Button type="button" variant="quiet" onClick={props.onCancel}>
        {t('取消')}
      </Button>
    </form>
  )
}
