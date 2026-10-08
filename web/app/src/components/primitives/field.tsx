import type { ReactNode } from 'react'
import { useTranslation } from '../../lib/i18n'

type FieldBase = {
  label: string
  name: string
  required?: boolean
  error?: string
  hint?: string
  disabled?: boolean
  className?: string
}

function FieldShell(props: FieldBase & { children: ReactNode }) {
  const { t } = useTranslation()
  // The <label> holds only the label text so hints/errors never leak into the accessible name;
  // they are linked through aria-describedby instead.
  return (
    <div
      className={`field ${props.className ?? ''}`.trim()}
      data-error={props.error ? 'true' : undefined}
    >
      <label className="field-label" htmlFor={props.name}>
        {t(props.label)}
      </label>
      {props.children}
      {props.hint && !props.error && (
        <span className="field-hint" id={`${props.name}-hint`}>
          {t(props.hint)}
        </span>
      )}
      {props.error && (
        <span className="field-error" id={`${props.name}-error`}>
          {t(props.error)}
        </span>
      )}
    </div>
  )
}

const describedBy = (props: FieldBase) =>
  props.error ? `${props.name}-error` : props.hint ? `${props.name}-hint` : undefined

/** Uncontrolled text input with label, hint and error wiring. */
export function Field(
  props: FieldBase & {
    type?: string
    defaultValue?: string | number
    placeholder?: string
    min?: number
    max?: number
    step?: number | string
    maxLength?: number
    autoComplete?: string
    readOnly?: boolean
  },
) {
  const { t } = useTranslation()
  return (
    <FieldShell {...props}>
      <input
        id={props.name}
        name={props.name}
        type={props.type ?? 'text'}
        required={props.required}
        disabled={props.disabled}
        readOnly={props.readOnly}
        defaultValue={props.defaultValue}
        placeholder={props.placeholder && t(props.placeholder)}
        min={props.min}
        max={props.max}
        step={props.step}
        maxLength={props.maxLength}
        autoComplete={props.autoComplete}
        aria-invalid={props.error ? true : undefined}
        aria-describedby={describedBy(props)}
      />
    </FieldShell>
  )
}

export function SelectField(
  props: FieldBase & {
    options: readonly { value: string; label: string }[]
    defaultValue?: string
  },
) {
  const { t } = useTranslation()
  return (
    <FieldShell {...props}>
      <select
        id={props.name}
        name={props.name}
        required={props.required}
        disabled={props.disabled}
        defaultValue={props.defaultValue}
        aria-invalid={props.error ? true : undefined}
        aria-describedby={describedBy(props)}
      >
        {props.options.map((option) => (
          <option key={option.value} value={option.value}>
            {t(option.label)}
          </option>
        ))}
      </select>
    </FieldShell>
  )
}

export function TextAreaField(
  props: FieldBase & { defaultValue?: string; placeholder?: string; rows?: number },
) {
  const { t } = useTranslation()
  return (
    <FieldShell {...props}>
      <textarea
        id={props.name}
        name={props.name}
        required={props.required}
        disabled={props.disabled}
        defaultValue={props.defaultValue}
        placeholder={props.placeholder && t(props.placeholder)}
        rows={props.rows ?? 4}
        aria-invalid={props.error ? true : undefined}
        aria-describedby={describedBy(props)}
      />
    </FieldShell>
  )
}
