import dayjs from 'dayjs'
import { useState } from 'react'
import type { APIKey, Schema } from '../lib/types'
import { useTranslation } from '../lib/i18n'
import { Button, ErrorMessage, Field, SelectField } from './ui'
import {
  keyInput,
  restrictionMode,
  scaledDecimal,
  type RestrictionMode,
} from '../features/keys/key-policy'
import { KeyRoutingField } from '../features/keys/routing-field'

export function KeyForm(props: {
  apiKey?: APIKey
  pending: boolean
  error?: Error | null
  initialGroup?: string
  onSave: (body: Schema['KeyInput']) => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  const [budgetLimited, setBudgetLimited] = useState(props.apiKey?.budget_limited ?? false)
  const [unlimitedMultiplier, setUnlimitedMultiplier] = useState(
    String(props.apiKey?.max_marketplace_multiplier_ppm ?? '1000000') === '0',
  )
  const [models, setModels] = useState<RestrictionMode>(
    restrictionMode(props.apiKey?.allowed_models),
  )
  const [networks, setNetworks] = useState<RestrictionMode>(
    restrictionMode(props.apiKey?.allowed_cidrs),
  )
  const [validation, setValidation] = useState<Error | null>(null)
  const savedMultiplier = props.apiKey?.max_marketplace_multiplier_ppm ?? 1_000_000
  const finiteMultiplier = String(savedMultiplier) === '0' ? 1_000_000 : savedMultiplier
  const restrictionOptions = [
    { value: 'all', label: '全部允许' },
    { value: 'selected', label: '指定列表' },
    { value: 'none', label: '全部禁止' },
  ]
  return (
    <form
      className="form-panel"
      id="key-form"
      onSubmit={(event) => {
        event.preventDefault()
        try {
          const body = keyInput(new FormData(event.currentTarget), props.apiKey)
          setValidation(null)
          props.onSave(body)
        } catch (cause) {
          setValidation(cause instanceof Error ? cause : new Error('请检查输入'))
        }
      }}
    >
      <Field name="name" label="名称" required maxLength={100} defaultValue={props.apiKey?.name} />
      <SelectField
        name="status"
        label="状态"
        defaultValue={props.apiKey?.status ?? 'active'}
        options={[
          { value: 'active', label: '启用' },
          { value: 'disabled', label: '停用' },
        ]}
      />
      <KeyRoutingField defaultValue={props.apiKey?.group ?? props.initialGroup} />
      <label className="checkbox-field">
        <input
          type="checkbox"
          name="cross_group_retry"
          defaultChecked={props.apiKey?.cross_group_retry}
        />
        {t('跨分组重试')}
      </label>
      <label className="checkbox-field">
        <input
          type="checkbox"
          name="multiplier_unlimited"
          checked={unlimitedMultiplier}
          onChange={(event) => setUnlimitedMultiplier(event.target.checked)}
        />
        {t('市场分组倍率不限')}
      </label>
      {!unlimitedMultiplier && (
        <Field
          name="max_multiplier"
          label="市场分组最大倍率"
          required
          type="text"
          defaultValue={scaledDecimal(finiteMultiplier)}
        />
      )}
      <label className="checkbox-field">
        <input
          type="checkbox"
          name="budget_limited"
          checked={budgetLimited}
          onChange={(event) => setBudgetLimited(event.target.checked)}
        />
        {t('限制 Key 额度')}
      </label>
      {budgetLimited && (
        <Field
          name="budget"
          label="Key 剩余额度（credits）"
          required
          type="text"
          defaultValue={scaledDecimal(props.apiKey?.budget_micro_credits)}
        />
      )}
      <Field
        name="expires_at"
        label="到期时间"
        type="datetime-local"
        defaultValue={
          props.apiKey?.expires_at
            ? dayjs(props.apiKey.expires_at).format('YYYY-MM-DDTHH:mm:ss')
            : ''
        }
        step={1}
      />
      <div className="field">
        <label className="field-label" htmlFor="models_mode">
          {t('模型访问')}
        </label>
        <select
          id="models_mode"
          name="models_mode"
          value={models}
          onChange={(event) => setModels(event.target.value as RestrictionMode)}
        >
          {restrictionOptions.map((option) => (
            <option key={option.value} value={option.value}>
              {t(option.label)}
            </option>
          ))}
        </select>
      </div>
      {models === 'selected' && (
        <Field
          name="models"
          label="允许的模型"
          required
          defaultValue={props.apiKey?.allowed_models?.join(', ')}
        />
      )}
      <div className="field">
        <label className="field-label" htmlFor="cidrs_mode">
          {t('网络访问')}
        </label>
        <select
          id="cidrs_mode"
          name="cidrs_mode"
          value={networks}
          onChange={(event) => setNetworks(event.target.value as RestrictionMode)}
        >
          {restrictionOptions.map((option) => (
            <option key={option.value} value={option.value}>
              {t(option.label)}
            </option>
          ))}
        </select>
      </div>
      {networks === 'selected' && (
        <Field
          name="cidrs"
          label="允许的网络"
          required
          defaultValue={props.apiKey?.allowed_cidrs?.join(', ')}
        />
      )}
      <ErrorMessage error={validation ?? props.error} />
      <div className="form-actions">
        <Button variant="quiet" type="button" onClick={props.onCancel}>
          {t('取消')}
        </Button>
        <Button disabled={props.pending} loading={props.pending} type="submit">
          {t(props.apiKey ? '保存' : '创建')}
        </Button>
      </div>
    </form>
  )
}
