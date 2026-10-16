/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useId } from 'react'
import { RotateCcw, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { ParameterEnabled, PlaygroundConfig } from '../types'

interface PlaygroundParametersProps {
  config: PlaygroundConfig
  enabled: ParameterEnabled
  disabled: boolean
  onConfigChange: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
  onEnabledChange: (key: keyof ParameterEnabled, value: boolean) => void
  onReset: () => void
  apiKey: string
  onApiKeyChange: (value: string) => void
}

const parameters = [
  { key: 'temperature', min: 0, max: 2, step: 0.1 },
  { key: 'top_p', min: 0, max: 1, step: 0.05 },
  { key: 'max_tokens', min: 1, max: undefined, step: 1 },
  { key: 'frequency_penalty', min: -2, max: 2, step: 0.1 },
  { key: 'presence_penalty', min: -2, max: 2, step: 0.1 },
  { key: 'seed', min: undefined, max: undefined, step: 1 },
] as const

export function PlaygroundParameters(props: PlaygroundParametersProps) {
  const { t } = useTranslation()
  const systemId = useId()
  const keyId = useId()
  return (
    <div className='pg-parameter-content'>
      <div className='pg-panel-heading'>
        <h2>{t('参数')}</h2>
        <button
          type='button'
          className='pg-icon-button'
          onClick={props.onReset}
          disabled={props.disabled}
          aria-label={t('Reset')}
        >
          <RotateCcw size={18} aria-hidden='true' />
        </button>
      </div>
      <div className='pg-field'>
        <label htmlFor={systemId}>{t('System prompt')}</label>
        <textarea
          id={systemId}
          rows={5}
          value={props.config.systemPrompt || ''}
          onChange={(event) =>
            props.onConfigChange('systemPrompt', event.target.value)
          }
          disabled={props.disabled}
        />
      </div>
      <label className='pg-toggle'>
        <span>{t('Stream')}</span>
        <input
          type='checkbox'
          checked={props.config.stream}
          onChange={(event) =>
            props.onConfigChange('stream', event.target.checked)
          }
          disabled={props.disabled}
        />
      </label>
      <div className='pg-parameter-fields'>
        {parameters.slice(0, 3).map((parameter) => (
          <div className='pg-parameter' key={parameter.key}>
            <label className='pg-toggle'>
              <span>{parameter.key}</span>
              <input
                type='checkbox'
                checked={props.enabled[parameter.key]}
                onChange={(event) =>
                  props.onEnabledChange(parameter.key, event.target.checked)
                }
                disabled={props.disabled}
              />
            </label>
            <input
              type='number'
              aria-label={parameter.key}
              min={parameter.min}
              max={parameter.max}
              step={parameter.step}
              value={props.config[parameter.key] ?? ''}
              disabled={props.disabled || !props.enabled[parameter.key]}
              onChange={(event) => {
                const value = event.target.valueAsNumber
                if (Number.isFinite(value) && event.target.validity.valid)
                  props.onConfigChange(parameter.key, value)
              }}
            />
          </div>
        ))}
      </div>
      <details className='pg-advanced'>
        <summary>{t('高级参数')}</summary>
        {parameters.slice(3).map((parameter) => (
          <div className='pg-parameter' key={parameter.key}>
            <label className='pg-toggle'>
              <span>{parameter.key}</span>
              <input
                type='checkbox'
                checked={props.enabled[parameter.key]}
                onChange={(event) =>
                  props.onEnabledChange(parameter.key, event.target.checked)
                }
                disabled={props.disabled}
              />
            </label>
            <input
              type='number'
              aria-label={parameter.key}
              min={parameter.min}
              max={parameter.max}
              step={parameter.step}
              value={props.config[parameter.key] ?? ''}
              disabled={props.disabled || !props.enabled[parameter.key]}
              onChange={(event) => {
                const value = event.target.valueAsNumber
                if (Number.isFinite(value) && event.target.validity.valid)
                  props.onConfigChange(parameter.key, value)
              }}
            />
          </div>
        ))}
      </details>
      <div className='pg-field pg-key-field'>
        <label htmlFor={keyId}>{t('API Key')}</label>
        <div className='pg-key-input'>
          <input
            id={keyId}
            type='password'
            autoComplete='off'
            spellCheck={false}
            value={props.apiKey}
            onChange={(event) => props.onApiKeyChange(event.target.value)}
            disabled={props.disabled}
          />
          {props.apiKey && (
            <button
              type='button'
              aria-label={t('Clear')}
              onClick={() => props.onApiKeyChange('')}
              disabled={props.disabled}
            >
              <X size={18} aria-hidden='true' />
            </button>
          )}
        </div>
        <span className='pg-endpoint'>
          {props.apiKey ? '/v1/chat/completions' : '/pg/chat/completions'}
        </span>
      </div>
      <div className='pg-status-row'>
        <span>tools</span>
        <span>{t('待接入')}</span>
      </div>
    </div>
  )
}
