import dayjs from 'dayjs'
import type { APIKey, Schema } from '../lib/types'
import { useTranslation } from '../lib/i18n'
import { Button, Field } from './ui'

export function KeyForm(props: {
  apiKey?: APIKey
  pending: boolean
  onSave: (body: Schema['KeyInput']) => void
  onCancel: () => void
}) {
  const { t } = useTranslation()
  return (
    <form
      className="form-panel"
      id="key-form"
      onSubmit={(event) => {
        event.preventDefault()
        const fields = new FormData(event.currentTarget)
        const list = (name: string, previous: string[] | null | undefined) => {
          const values = String(fields.get(name) ?? '')
            .split(',')
            .map((value) => value.trim())
            .filter(Boolean)
          // Null is unrestricted; an existing empty list intentionally denies all.
          return values.length ? values : previous?.length === 0 ? [] : null
        }
        const expiration = String(fields.get('expires_at') ?? '')
        props.onSave({
          id: props.apiKey?.id ?? 0,
          name: String(fields.get('name')),
          status: props.apiKey?.status ?? 'active',
          group: props.apiKey?.group ?? null,
          allowed_models: list('models', props.apiKey?.allowed_models),
          allowed_cidrs: list('cidrs', props.apiKey?.allowed_cidrs),
          expires_at: expiration ? new Date(expiration).toISOString() : null,
          budget_limited: props.apiKey?.budget_limited ?? false,
          budget_micro_credits: props.apiKey?.budget_micro_credits ?? null,
          cross_group_retry: props.apiKey?.cross_group_retry ?? false,
          max_marketplace_multiplier_ppm: props.apiKey?.max_marketplace_multiplier_ppm ?? 1_000_000,
        })
      }}
    >
      <Field name="name" label="名称" required maxLength={100} defaultValue={props.apiKey?.name} />
      <Field
        name="models"
        label="允许的模型"
        placeholder="gpt-4o, claude-sonnet-4"
        hint={
          props.apiKey?.allowed_models?.length === 0
            ? '当前禁止全部模型；填写模型名以开放访问。'
            : '留空表示允许全部模型'
        }
        defaultValue={props.apiKey?.allowed_models?.join(', ')}
      />
      <Field
        name="cidrs"
        label="允许的网络"
        placeholder="192.0.2.0/24"
        hint={
          props.apiKey?.allowed_cidrs?.length === 0
            ? '当前禁止全部网络；填写网段以开放访问。'
            : '留空表示允许全部网络'
        }
        defaultValue={props.apiKey?.allowed_cidrs?.join(', ')}
      />
      <Field
        name="expires_at"
        label="到期时间"
        type="datetime-local"
        defaultValue={
          props.apiKey?.expires_at ? dayjs(props.apiKey.expires_at).format('YYYY-MM-DDTHH:mm') : ''
        }
      />
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
