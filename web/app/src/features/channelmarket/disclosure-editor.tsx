import { useId, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, unwrap } from '../../lib/api'
import { resourceOptions } from '../../lib/queries'
import { useTranslation } from '../../lib/i18n'
import type { Schema } from '../../lib/types'
import { Button, ErrorMessage, Field, Loading, SelectField } from '../../components/ui'
import { MarketForm, text } from './form'
import './disclosure-editor.css'

const sourceOptions = [
  { value: 'unknown', label: '未说明' },
  { value: 'direct', label: '直接上游' },
  { value: 'reseller', label: '转售或聚合上游' },
  { value: 'self_hosted', label: '自行部署模型' },
] as const
const retentionOptions = [
  { value: 'unknown', label: '尚未确认' },
  { value: 'none', label: '声明不保留请求内容' },
  { value: 'limited', label: '有限期保留请求内容' },
] as const
const trainingOptions = [
  { value: 'unknown', label: '尚未确认' },
  { value: 'no', label: '声明不用于训练' },
  { value: 'yes', label: '可能用于训练' },
] as const
const capabilityOptions = [
  { value: 'unknown', label: '尚未确认' },
  { value: 'supported', label: '支持' },
  { value: 'unsupported', label: '不支持' },
] as const
const capabilityFields = [
  { key: 'streaming', label: '流式输出' },
  { key: 'tools', label: '工具调用' },
  { key: 'structured_outputs', label: '结构化输出' },
  { key: 'vision', label: '视觉输入' },
] as const

function choice<const T extends readonly { value: string }[]>(
  fields: FormData,
  name: string,
  options: T,
): T[number]['value'] {
  const value = text(fields, name)
  const selected = options.find((option) => option.value === value)
  if (!selected) throw new Error('服务声明选项无效')
  return selected.value
}

function tokenLimit(fields: FormData, name: string): bigint | undefined {
  const value = text(fields, name)
  if (!value) return undefined
  if (!/^[1-9]\d*$/.test(value)) throw new Error('Token 上限必须为正整数，未知请留空')
  const integer = BigInt(value)
  if (integer > 1_000_000_000n) throw new Error('Token 上限不能超过十亿')
  return integer
}

/** Only editable claims enter the request; verification provenance stays server-owned. */
export function disclosureInput(
  fields: FormData,
  models: readonly string[],
  prefix = 'disclosure',
): Schema['ChannelMarketDisclosureInput'] {
  const name = (key: string) => `${prefix}-${key}`
  const regions = [
    ...new Set(
      text(fields, name('regions'))
        .toUpperCase()
        .split(/[\s,，;；]+/)
        .filter(Boolean),
    ),
  ]
  if (regions.some((region) => !/^[A-Z]{2}$/.test(region)))
    throw new Error('处理地区请填写两位国家代码，例如 HK、US、JP')
  const retention = choice(fields, name('retention'), retentionOptions)
  let retentionDays: number | undefined
  if (retention === 'limited') {
    const value = text(fields, name('retention-days'))
    if (value) {
      if (!/^\d+$/.test(value) || Number(value) > 3650)
        throw new Error('保留天数应为 0–3650 的整数')
      retentionDays = Number(value)
    }
  }
  const policyURL = text(fields, name('policy-url'))
  if (policyURL) {
    let parsed: URL
    try {
      parsed = new URL(policyURL)
    } catch {
      throw new Error('数据政策链接必须是有效的 HTTPS 地址')
    }
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password)
      throw new Error('数据政策链接必须是有效的 HTTPS 地址')
  }
  return {
    source_kind: choice(fields, name('source'), sourceOptions),
    regions,
    retention,
    ...(retentionDays !== undefined ? { retention_days: retentionDays } : {}),
    training: choice(fields, name('training'), trainingOptions),
    ...(policyURL ? { policy_url: policyURL } : {}),
    models: [...new Set(models)].map((model, index) => {
      const context = tokenLimit(fields, name(`model-${index}-context`))
      const output = tokenLimit(fields, name(`model-${index}-output`))
      if (context !== undefined && output !== undefined && output > context)
        throw new Error('输出上限不能超过上下文上限')
      return {
        model,
        streaming: choice(fields, name(`model-${index}-streaming`), capabilityOptions),
        tools: choice(fields, name(`model-${index}-tools`), capabilityOptions),
        structured_outputs: choice(
          fields,
          name(`model-${index}-structured_outputs`),
          capabilityOptions,
        ),
        vision: choice(fields, name(`model-${index}-vision`), capabilityOptions),
        context_tokens: context,
        max_output_tokens: output,
      }
    }),
  }
}

export const disclosureOptions = (id: string) => ({
  ...resourceOptions(
    'market-disclosure',
    (signal) =>
      api
        .GET('/api/marketplace/channels/{id}/disclosure', {
          signal,
          params: { path: { id } },
        })
        .then(unwrap),
    [id],
  ),
  staleTime: 60_000,
})

export function DisclosureEditor(props: { id: string; models: readonly string[] }) {
  const { t, locale } = useTranslation()
  const client = useQueryClient()
  const query = useQuery(disclosureOptions(props.id))
  const [saved, setSaved] = useState(false)
  const save = useMutation({
    mutationFn: (body: Schema['ChannelMarketDisclosureInput']) =>
      api
        .PUT('/api/marketplace/channels/{id}/disclosure', {
          params: { path: { id: props.id } },
          body,
        })
        .then(unwrap),
    onMutate: () => setSaved(false),
    onSuccess: (data) => {
      client.setQueryData(disclosureOptions(props.id).queryKey, data)
      void client.invalidateQueries({ queryKey: ['market-model-insights'] })
      setSaved(true)
    },
  })
  return (
    <section className="disclosure-editor section">
      <h3>{t('服务声明与模型能力')}</h3>
      <p className="subtle">
        {t('声明由渠道主提供，供用户比较。连接验证不代表来源授权、模型真实性或数据政策已获核验。')}
      </p>
      <p className="field-hint">
        {t('请仅填写能够确认的内容；未知项目保留“尚未确认”，不要以零值代表未知。')}
      </p>
      <ErrorMessage error={query.error ?? save.error} />
      {query.isPending && <Loading />}
      {query.isError && (
        <Button variant="quiet" disabled={query.isFetching} onClick={() => void query.refetch()}>
          {t('重试')}
        </Button>
      )}
      {!query.isPending && !query.isError && (
        <DisclosureFields
          key={`${props.id}:${query.data?.updated_at ?? 'new'}:${props.models.join('\n')}`}
          disclosure={query.data ?? undefined}
          models={props.models}
          pending={save.isPending}
          onSave={(body) => save.mutate(body)}
        />
      )}
      {query.data?.updated_at && (
        <p className="field-hint">
          {t('上次声明更新')} · {new Date(query.data.updated_at).toLocaleString(locale)}
        </p>
      )}
      {saved && (
        <p className="notice" role="status">
          {t('服务声明已保存')}
        </p>
      )}
    </section>
  )
}

function DisclosureFields(props: {
  disclosure?: Schema['ChannelMarketDisclosure']
  models: readonly string[]
  pending: boolean
  onSave: (body: Schema['ChannelMarketDisclosureInput']) => void
}) {
  const { t } = useTranslation()
  const prefix = `disclosure-${useId()}`
  const name = (key: string) => `${prefix}-${key}`
  const models = [...new Set(props.models)]
  const [retention, setRetention] = useState(props.disclosure?.retention ?? 'unknown')
  return (
    <MarketForm
      pending={props.pending}
      submit="保存服务声明"
      onSubmit={(fields) => props.onSave(disclosureInput(fields, models, prefix))}
    >
      <fieldset className="disclosure-grid" disabled={props.pending}>
        <legend>{t('来源与数据处理')}</legend>
        <SelectField
          name={name('source')}
          label="上游来源"
          options={sourceOptions}
          defaultValue={props.disclosure?.source_kind ?? 'unknown'}
        />
        <Field
          name={name('regions')}
          label="处理地区"
          defaultValue={props.disclosure?.regions?.join(', ')}
          placeholder="HK, US, JP"
          hint="使用两位国家代码，多个地区用逗号分隔；未知请留空。"
        />
        <label className="field" htmlFor={name('retention')}>
          <span>{t('请求内容保留')}</span>
          <select
            id={name('retention')}
            name={name('retention')}
            value={retention}
            onChange={(event) => {
              const selected = retentionOptions.find(
                (option) => option.value === event.target.value,
              )
              if (selected) setRetention(selected.value)
            }}
          >
            {retentionOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {t(option.label)}
              </option>
            ))}
          </select>
        </label>
        {retention === 'limited' && (
          <Field
            name={name('retention-days')}
            label="保留天数"
            type="number"
            min={0}
            max={3650}
            step={1}
            defaultValue={props.disclosure?.retention_days}
            hint="未确认具体保留期限时请留空。"
          />
        )}
        <SelectField
          name={name('training')}
          label="请求内容用于训练"
          options={trainingOptions}
          defaultValue={props.disclosure?.training ?? 'unknown'}
        />
        <Field
          name={name('policy-url')}
          label="数据政策链接（可选）"
          type="url"
          defaultValue={props.disclosure?.policy_url}
          maxLength={500}
          hint="仅填写可公开核对的数据政策 HTTPS 链接；不得用来发布广告或联系方式。"
        />
      </fieldset>
      <div className="disclosure-models">
        <h4>{t('各模型能力')}</h4>
        <p className="field-hint">
          {t('按当前渠道中的模型逐项填写；修改模型列表后，请重新检查声明。')}
        </p>
        {!models.length && <p className="muted">{t('添加模型后可填写对应能力。')}</p>}
        {models.map((model, index) => {
          const current = props.disclosure?.models?.find((entry) => entry.model === model)
          return (
            <fieldset
              key={model}
              className="disclosure-grid disclosure-model"
              disabled={props.pending}
            >
              <legend>
                <code dir="ltr">{model}</code>
              </legend>
              {capabilityFields.map((capability) => (
                <SelectField
                  key={capability.key}
                  name={name(`model-${index}-${capability.key}`)}
                  label={capability.label}
                  options={capabilityOptions}
                  defaultValue={current?.[capability.key] ?? 'unknown'}
                />
              ))}
              <Field
                name={name(`model-${index}-context`)}
                label="上下文上限（Token）"
                defaultValue={current?.context_tokens?.toString()}
                maxLength={10}
                hint="仅填正整数；未知请留空。"
              />
              <Field
                name={name(`model-${index}-output`)}
                label="输出上限（Token）"
                defaultValue={current?.max_output_tokens?.toString()}
                maxLength={10}
                hint="仅填正整数；未知请留空。"
              />
            </fieldset>
          )
        })}
      </div>
    </MarketForm>
  )
}
