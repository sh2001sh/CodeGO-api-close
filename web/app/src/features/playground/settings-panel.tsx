import { useTranslation } from '../../lib/i18n'
import type { PlaygroundSettings } from './types'

/**
 * Live-controlled settings fields. The shared `Field`/`TextAreaField` primitives are
 * uncontrolled (defaultValue only), but the playground needs current values on every
 * keystroke to build the request and the "查看代码" sample, so plain inputs are used
 * here with the same `.field` markup/classes for visual consistency.
 */
export function SettingsPanel(props: {
  settings: PlaygroundSettings
  onChange: (settings: PlaygroundSettings) => void
  models: string[]
  modelsPending: boolean
  mode?: 'model' | 'advanced'
  disabled?: boolean
  groups?: string[]
  groupNames?: Record<string, string>
}) {
  const { t } = useTranslation()
  const update = (patch: Partial<PlaygroundSettings>) =>
    props.onChange({ ...props.settings, ...patch })
  return (
    <div className="playground-settings">
      {props.mode !== 'advanced' && (
        <>
          {props.groups && (
            <div className="field">
              <label className="field-label" htmlFor="playground-group">
                {t('分组')}
              </label>
              <select
                id="playground-group"
                value={props.settings.group ?? ''}
                disabled={props.disabled}
                onChange={(event) => update({ group: event.target.value, model: '' })}
              >
                <option value="">{t('Key 默认分组')}</option>
                {props.groups.map((group) => (
                  <option key={group} value={group}>
                    {props.groupNames?.[group] ?? group}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div className="field">
            <label className="field-label" htmlFor="playground-model">
              {t('模型')}
            </label>
            <input
              id="playground-model"
              list="playground-model-options"
              value={props.settings.model}
              disabled={props.disabled}
              onChange={(event) => update({ model: event.target.value })}
              placeholder={props.modelsPending ? t('正在加载') : t('选择模型')}
            />
            <datalist id="playground-model-options">
              {props.models.map((model) => (
                <option key={model} value={model} />
              ))}
            </datalist>
          </div>
        </>
      )}
      {props.mode !== 'model' && (
        <>
          <div className="field">
            <label className="field-label" htmlFor="playground-temperature">
              {t('温度')}
            </label>
            <input
              id="playground-temperature"
              placeholder="0.0 - 2.0"
              type="number"
              min="0"
              max="2"
              step="0.1"
              disabled={props.disabled}
              value={props.settings.temperature}
              onChange={(event) => update({ temperature: event.target.value })}
            />
          </div>
          <div className="field">
            <label className="field-label" htmlFor="playground-max-tokens">
              {t('最大输出长度')}
            </label>
            <input
              id="playground-max-tokens"
              placeholder="1024"
              type="number"
              min="1"
              step="1"
              disabled={props.disabled}
              value={props.settings.maxTokens}
              onChange={(event) => update({ maxTokens: event.target.value })}
            />
          </div>
          <div className="field">
            <label className="field-label" htmlFor="playground-system">
              {t('系统提示词')}
            </label>
            <textarea
              id="playground-system"
              rows={5}
              disabled={props.disabled}
              value={props.settings.systemPrompt}
              onChange={(event) => update({ systemPrompt: event.target.value })}
            />
          </div>
        </>
      )}
    </div>
  )
}
