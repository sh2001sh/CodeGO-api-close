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
import { ArrowUp, Paperclip, Square, ArrowUpRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

interface PlaygroundInputProps {
  onSubmit: (text: string) => void
  onStop: () => void
  disabled?: boolean
  isGenerating: boolean
  value: string
  onChange: (value: string) => void
  empty: boolean
}

export function PlaygroundInput(props: PlaygroundInputProps) {
  const { t } = useTranslation()
  const send = () => {
    if (!props.value.trim() || props.disabled || props.isGenerating) return
    props.onSubmit(props.value)
    props.onChange('')
  }
  return (
    <div className='pg-composer-area'>
      {props.empty && (
        <div className='pg-suggestions'>
          {[
            'Analyze data',
            'Surprise me',
            'Summarize text',
            'Code',
            'Get advice',
            'More',
          ].map((suggestion) => (
            <button
              key={suggestion}
              type='button'
              disabled={props.disabled || props.isGenerating}
              onClick={() => props.onChange(t(suggestion))}
            >
              {t(suggestion)}
              <ArrowUpRight size={15} aria-hidden='true' />
            </button>
          ))}
        </div>
      )}
      <form
        className='pg-composer'
        onSubmit={(event) => {
          event.preventDefault()
          send()
        }}
      >
        <label htmlFor='pg-message' className='pg-composer-label'>
          {t('消息')}
        </label>
        <textarea
          id='pg-message'
          value={props.value}
          rows={3}
          autoComplete='off'
          autoCorrect='off'
          autoCapitalize='off'
          spellCheck={false}
          onChange={(event) => props.onChange(event.target.value)}
          onKeyDown={(event) => {
            if (
              event.key === 'Enter' &&
              !event.shiftKey &&
              !event.nativeEvent.isComposing
            ) {
              event.preventDefault()
              send()
            }
          }}
        />
        <div className='pg-composer-controls'>
          <button
            className='pg-attachment'
            type='button'
            disabled
            aria-label={t('附件 · 待接入')}
          >
            <Paperclip size={19} aria-hidden='true' />
            <span>{t('附件')}</span>
            <span>{t('待接入')}</span>
          </button>
          {props.isGenerating ? (
            <button className='pg-send' type='button' onClick={props.onStop}>
              <Square size={17} aria-hidden='true' />
              {t('Stop')}
            </button>
          ) : (
            <button
              className='pg-send'
              type='submit'
              disabled={props.disabled || !props.value.trim()}
            >
              <ArrowUp size={20} aria-hidden='true' />
              {t('Send')}
            </button>
          )}
        </div>
      </form>
    </div>
  )
}
