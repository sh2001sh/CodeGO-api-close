// Shared filter bar for analytics pages: time-range presets + custom range +
// model/user/key/channel text filters. Pages decide which optional fields to render.
// Text fields are uncontrolled (read from FormData on submit) to match the
// project convention for Field/SelectField/TextAreaField.
import { useState } from 'react'
import { useTranslation } from '../../lib/i18n'
import { Button, Field } from '../../components/ui'
import type { UsageFilters } from './types'
import {
  isoToLocalInput,
  localInputToISO,
  rangeFromPreset,
  rangePresetOptions,
  type Range,
  type RangePreset,
} from './time-range'

export function UsageFilterBar(props: {
  filters: UsageFilters
  range: Range
  preset: RangePreset
  onChange: (filters: UsageFilters, range: Range, preset: RangePreset) => void
  showUser?: boolean
  showKey?: boolean
  showChannel?: boolean
}) {
  const { t } = useTranslation()
  const [preset, setPreset] = useState(props.preset)
  const [customFrom, setCustomFrom] = useState(isoToLocalInput(props.range.from))
  const [customTo, setCustomTo] = useState(isoToLocalInput(props.range.to))
  const readFilters = (form: HTMLFormElement): UsageFilters => {
    const data = new FormData(form)
    return {
      model: String(data.get('model') ?? ''),
      userID: String(data.get('user-id') ?? ''),
      keyID: String(data.get('key-id') ?? ''),
      channelID: String(data.get('channel-id') ?? ''),
    }
  }
  const resolveRange = (nextPreset: RangePreset): Range =>
    nextPreset === 'custom'
      ? { from: localInputToISO(customFrom), to: localInputToISO(customTo) }
      : rangeFromPreset(nextPreset)
  return (
    <form
      className="filters analytics-filter-bar"
      onSubmit={(event) => {
        event.preventDefault()
        props.onChange(readFilters(event.currentTarget), resolveRange(preset), preset)
      }}
    >
      <div className="segmented" role="group" aria-label={t('时间范围')}>
        {rangePresetOptions.map((option) => (
          <button
            key={option.value}
            type="button"
            aria-pressed={preset === option.value}
            onClick={(event) => {
              setPreset(option.value)
              if (option.value !== 'custom')
                props.onChange(
                  readFilters(event.currentTarget.form!),
                  rangeFromPreset(option.value),
                  option.value,
                )
            }}
          >
            {t(option.label)}
          </button>
        ))}
      </div>
      {preset === 'custom' && (
        <>
          <label className="field" htmlFor="range-from">
            <span>{t('起始时间')}</span>
            <input
              id="range-from"
              type="datetime-local"
              value={customFrom}
              onChange={(event) => setCustomFrom(event.target.value)}
            />
          </label>
          <label className="field" htmlFor="range-to">
            <span>{t('结束时间')}</span>
            <input
              id="range-to"
              type="datetime-local"
              value={customTo}
              onChange={(event) => setCustomTo(event.target.value)}
            />
          </label>
        </>
      )}
      <Field name="model" label="模型" placeholder="gpt-4o" defaultValue={props.filters.model} />
      {props.showUser && (
        <Field
          name="user-id"
          label="用户 ID"
          placeholder="12345"
          defaultValue={props.filters.userID}
        />
      )}
      {props.showKey && (
        <Field
          name="key-id"
          label="密钥 ID"
          placeholder="12345"
          defaultValue={props.filters.keyID}
        />
      )}
      {props.showChannel && (
        <Field
          name="channel-id"
          label="渠道 ID"
          placeholder="12345"
          defaultValue={props.filters.channelID}
        />
      )}
      <Button type="submit">{t('筛选')}</Button>
    </form>
  )
}
