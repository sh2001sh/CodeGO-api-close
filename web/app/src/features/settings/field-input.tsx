// Renders a single typed settings field: boolean switch, masked secret with
// reveal-to-replace, JSON textarea, or plain text/number input. Values are
// uncontrolled (defaultValue/defaultChecked) so the parent form reads them
// from FormData on submit, matching the rest of the codebase's form style.
import { useTranslation } from '../../lib/i18n'
import { Badge, Button, Field, TextAreaField } from '../../components/ui'
import type { Schema } from '../../lib/types'
import type { SettingFieldDef } from './registry-types'
import { initialBooleanValue, initialTextValue } from './values'

type Setting = Schema['CatalogSetting']

export function SettingFieldInput(props: {
  field: SettingFieldDef
  setting?: Setting
  error?: string
  replacing: boolean
  onToggleReplace: () => void
}) {
  const { t } = useTranslation()
  const { field, setting } = props

  if (field.type === 'secret') {
    const configured = setting?.configured ?? false
    if (!props.replacing) {
      return (
        <div className="field">
          <span className="field-label">{t(field.label)}</span>
          <div className="row-actions">
            <Badge tone={configured ? 'success' : 'neutral'}>
              {t(configured ? '已配置' : '未配置')}
            </Badge>
            <Button type="button" variant="quiet" size="sm" onClick={props.onToggleReplace}>
              {t(configured ? '替换' : '设置')}
            </Button>
          </div>
          {field.description && <span className="field-hint">{t(field.description)}</span>}
        </div>
      )
    }
    return (
      <div className="field" data-error={props.error ? 'true' : undefined}>
        <label className="field-label" htmlFor={field.key}>
          {t(field.label)}
        </label>
        <div className="row-actions">
          <input
            id={field.key}
            name={field.key}
            type="password"
            autoComplete="new-password"
            placeholder={t('输入新值')}
            aria-invalid={props.error ? true : undefined}
          />
          <Button type="button" variant="quiet" size="sm" onClick={props.onToggleReplace}>
            {t('取消')}
          </Button>
        </div>
        {props.error && <span className="field-error">{t(props.error)}</span>}
      </div>
    )
  }

  if (field.type === 'boolean') {
    return (
      <div className="field">
        <label className="checkbox-field" htmlFor={field.key}>
          <input
            id={field.key}
            name={field.key}
            type="checkbox"
            defaultChecked={initialBooleanValue(field, setting?.value)}
          />
          {t(field.label)}
        </label>
        {field.description && <span className="field-hint">{t(field.description)}</span>}
      </div>
    )
  }

  const value = initialTextValue(field, setting?.value)
  if (field.type === 'json') {
    return (
      <TextAreaField
        name={field.key}
        label={field.label}
        hint={field.hint ?? field.description}
        error={props.error}
        rows={4}
        defaultValue={value}
      />
    )
  }
  return (
    <Field
      name={field.key}
      label={field.label}
      type={field.type === 'number' ? 'number' : 'text'}
      hint={field.hint ?? field.description}
      error={props.error}
      placeholder={field.placeholder}
      defaultValue={value}
    />
  )
}
