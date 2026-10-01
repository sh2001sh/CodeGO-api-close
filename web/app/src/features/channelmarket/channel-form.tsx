import { Button, Field } from '../../components/ui'
import { useState } from 'react'
import type { Schema } from '../../lib/types'
import { MarketForm, factor, text } from './form'

export function channelPatch(
  body: Schema['ChannelMarketCreateInput'],
  previous: Schema['ChannelMarketChannelView'],
  updateService: boolean,
): Schema['ChannelMarketPatchInput'] {
  return {
    name: body.name,
    source_label: body.source_label,
    model_prices: body.model_prices,
    multiplier: body.multiplier,
    visibility: body.visibility,
    ...(JSON.stringify(body.declared_models) !== JSON.stringify(previous.declared_models)
      ? { declared_models: body.declared_models }
      : {}),
    ...(body.provider_type !== previous.provider_type ? { provider_type: body.provider_type } : {}),
    ...(body.base_url ? { base_url: body.base_url } : {}),
    ...(body.api_key ? { api_key: body.api_key } : {}),
    ...(updateService
      ? {
          max_concurrency: body.max_concurrency,
          user_max_concurrency: body.user_max_concurrency,
          qps: body.qps,
          maintenance_window: body.maintenance_window,
          sensitive_word_interception_enabled: body.sensitive_word_interception_enabled,
          multiplier_card_supported: body.multiplier_card_supported,
          multiplier_card_user_enabled: body.multiplier_card_user_enabled,
          auto_probe_enabled: body.auto_probe_enabled,
          auto_probe_interval_minutes: body.auto_probe_interval_minutes,
          auto_probe_model: body.auto_probe_model,
        }
      : {}),
  }
}

export function MarketChannelForm(props: {
  channel?: Schema['ChannelMarketChannelView']
  pending: boolean
  onSave: (input: Schema['ChannelMarketCreateInput'], updateService: boolean) => void
  onCancel: () => void
}) {
  const channel = props.channel
  const [updateService, setUpdateService] = useState(!channel)
  return (
    <section className="section">
      <h2>{channel ? '编辑渠道' : '提交渠道'}</h2>
      <MarketForm
        pending={props.pending}
        onSubmit={(fields) => {
          const prices = JSON.parse(
            text(fields, 'model_prices') || '{}',
          ) as Schema['ChannelMarketCreateInput']['model_prices']
          if (!prices || typeof prices !== 'object' || Array.isArray(prices))
            throw new Error('模型价格必须是 JSON 对象')
          props.onSave(
            {
              name: text(fields, 'name'),
              provider_type: text(fields, 'provider_type'),
              source_label: text(fields, 'source_label'),
              base_url: text(fields, 'base_url'),
              api_key: text(fields, 'api_key'),
              declared_models: text(fields, 'models')
                .split(',')
                .map((model) => model.trim())
                .filter(Boolean),
              model_prices: prices,
              multiplier: factor(fields, 'channel-multiplier'),
              visibility: text(fields, 'visibility'),
              max_concurrency: Number(text(fields, 'max_concurrency')),
              user_max_concurrency: Number(text(fields, 'user_max_concurrency')),
              qps: Number(text(fields, 'qps')),
              maintenance_window: text(fields, 'maintenance_window'),
              sensitive_word_interception_enabled: fields.has('interception'),
              multiplier_card_supported: fields.has('cards'),
              multiplier_card_user_enabled: fields.has('cards'),
              auto_probe_enabled: fields.has('auto_probe'),
              auto_probe_interval_minutes: Number(text(fields, 'probe_interval')),
              auto_probe_model: text(fields, 'probe_model'),
            },
            updateService,
          )
        }}
      >
        <Field
          name="name"
          label="渠道名称"
          required
          defaultValue={channel?.system_display_name}
          maxLength={255}
        />
        <Field
          name="provider_type"
          label="Provider"
          required
          defaultValue={channel?.provider_type ?? 'openai'}
        />
        <Field
          name="source_label"
          label="来源说明"
          defaultValue={channel?.approved_source_label}
          maxLength={40}
        />
        <Field
          name="base_url"
          label={channel ? '更新上游地址（留空保持）' : '上游地址'}
          type="url"
          required={!channel}
        />
        <Field
          name="api_key"
          label={channel ? '更新凭据（留空保持）' : '上游凭据'}
          type="password"
          required={!channel}
        />
        <Field
          name="models"
          label="模型（逗号分隔）"
          required
          defaultValue={channel?.declared_models?.join(', ')}
        />
        <Field
          name="channel-multiplier"
          label="公开倍率"
          required
          defaultValue={String(channel?.multiplier ?? 1)}
        />
        <label className="field" htmlFor="market-visibility">
          <span>可见范围</span>
          <select
            id="market-visibility"
            name="visibility"
            defaultValue={channel?.visibility ?? 'private'}
          >
            <option value="private">私有 · 仅受邀用户</option>
            <option value="public">公开</option>
            <option value="unlisted">不列出 · 已授权用户</option>
          </select>
        </label>
        <label className="field" htmlFor="market-prices" style={{ flexBasis: '100%' }}>
          <span>模型价格 JSON</span>
          <textarea
            id="market-prices"
            name="model_prices"
            rows={4}
            defaultValue={JSON.stringify(channel?.model_prices ?? {}, null, 2)}
          />
        </label>
        {channel && (
          <label>
            <input
              type="checkbox"
              checked={updateService}
              onChange={(event) => setUpdateService(event.target.checked)}
            />{' '}
            更新并发与服务策略（重新填写）
          </label>
        )}
        {updateService && (
          <>
            <Field
              name="max_concurrency"
              label="渠道并发（0 不限）"
              type="number"
              min={0}
              defaultValue={0}
            />
            <Field
              name="user_max_concurrency"
              label="用户并发（0 不限）"
              type="number"
              min={0}
              defaultValue={0}
            />
            <Field name="qps" label="每秒请求数（0 不限）" type="number" min={0} defaultValue={0} />
            <Field name="maintenance_window" label="维护时间" placeholder="02:00-03:00" />
            <Field
              name="probe_interval"
              label="自动探测间隔（分钟）"
              type="number"
              min={1}
              defaultValue={30}
            />
            <Field name="probe_model" label="自动探测模型" />
            <label>
              <input type="checkbox" name="auto_probe" /> 自动探测
            </label>
            <label>
              <input type="checkbox" name="interception" defaultChecked /> 敏感词拦截
            </label>
            <label>
              <input type="checkbox" name="cards" /> 支持倍率卡
            </label>
          </>
        )}
        <Button variant="quiet" type="button" onClick={props.onCancel}>
          取消
        </Button>
      </MarketForm>
      <p className="muted">连接和模型修改后需要重新验证与审核。</p>
    </section>
  )
}
