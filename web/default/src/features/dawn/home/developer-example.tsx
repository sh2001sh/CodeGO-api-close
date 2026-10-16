/* Copyright (C) 2023-2026 QuantumNous. AGPL-3.0-or-later. */
import { useEffect, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Check, Copy, ArrowRight, ChevronDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { HomeTabs } from './home-tabs'
import {
  createCodeExample,
  DEFAULT_ENDPOINT,
  endpointFormSchema,
  type CodeLanguage,
} from './lib/code-example'

const CODE_OPTIONS = [
  { value: 'curl', labelKey: 'marketHome.languageCurl' },
  { value: 'python', labelKey: 'marketHome.languagePython' },
  { value: 'javascript', labelKey: 'marketHome.languageJavaScript' },
] as const

function HighlightedCode(props: { source: string }) {
  const tokens =
    /(?:#[^\n]*|\/\/[^\n]*)|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\b(?:import|from|const|await|for|of|in|print|export)\b|--[a-zA-Z-]+/g
  const parts = []
  let previous = 0
  for (const match of props.source.matchAll(tokens)) {
    parts.push(props.source.slice(previous, match.index))
    const value = match[0]
    let className = 'mh-syntax-keyword'
    if (value.startsWith('#') || value.startsWith('//'))
      className = 'mh-syntax-comment'
    else if (value.startsWith('"') || value.startsWith("'"))
      className = 'mh-syntax-string'
    parts.push(
      <span key={match.index} className={className}>
        {value}
      </span>
    )
    previous = match.index + value.length
  }
  parts.push(props.source.slice(previous))
  return <code>{parts}</code>
}

export function DeveloperExample(props: { selectedModelId?: string }) {
  const { t } = useTranslation()
  const [language, setLanguage] = useState<CodeLanguage>('curl')
  const [endpoint, setEndpoint] = useState(DEFAULT_ENDPOINT)
  const [request, setRequest] = useState<'models' | 'chat'>('models')
  const [customModel, setCustomModel] = useState<string | null>(null)
  const [copyBusy, setCopyBusy] = useState(false)
  const [copyState, setCopyState] = useState<'idle' | 'success' | 'failed'>(
    'idle'
  )
  const [manualOpen, setManualOpen] = useState(false)
  const [saved, setSaved] = useState(false)
  const manualCode = useRef<HTMLTextAreaElement>(null)
  const form = useForm({
    resolver: zodResolver(endpointFormSchema),
    mode: 'onBlur',
    reValidateMode: 'onBlur',
    defaultValues: { endpoint: '' },
  })
  const errorKey = form.formState.errors.endpoint?.message
  const modelId = customModel ?? props.selectedModelId ?? ''
  const source = createCodeExample(
    language,
    endpoint,
    '',
    request === 'chat' ? modelId || 'MODEL_ID' : undefined
  )

  useEffect(() => {
    if (copyState !== 'success') return
    const timeout = window.setTimeout(() => setCopyState('idle'), 2000)
    return () => window.clearTimeout(timeout)
  }, [copyState])

  async function copyCode(): Promise<void> {
    if (copyBusy) return
    setCopyBusy(true)
    setCopyState('idle')
    try {
      await navigator.clipboard.writeText(source)
      setCopyState('success')
      setManualOpen(false)
    } catch {
      setCopyState('failed')
      setManualOpen(true)
      requestAnimationFrame(() => {
        manualCode.current?.focus()
        manualCode.current?.select()
      })
    } finally {
      setCopyBusy(false)
    }
  }

  function copyLabel(): string {
    if (copyBusy) return t('marketHome.copying')
    if (copyState === 'success') return t('marketHome.copied')
    if (copyState === 'failed') return t('复制失败')
    return t('marketHome.copy')
  }

  return (
    <div className='mh-developer'>
      <div className='mh-config'>
        <form
          noValidate
          onSubmit={form.handleSubmit((values) => {
            setEndpoint(values.endpoint)
            form.reset(values)
            setSaved(true)
          })}
        >
          <div className='mh-field'>
            <label htmlFor='endpoint'>Base URL</label>
            <input
              {...form.register('endpoint', {
                onChange: () => setSaved(false),
              })}
              id='endpoint'
              type='url'
              spellCheck={false}
              autoComplete='off'
              placeholder={t('待配置')}
              aria-required='true'
              aria-invalid={Boolean(errorKey)}
              aria-describedby={errorKey ? 'endpoint-error' : undefined}
            />
            {errorKey && (
              <p id='endpoint-error' className='mh-field-error' role='alert'>
                {t(errorKey)}
              </p>
            )}
          </div>
          <div className='mh-config-actions'>
            <button className='mh-button mh-outline' type='submit'>
              {t(saved ? '已更新' : '更新示例')}
              <ArrowRight aria-hidden='true' />
            </button>
            <button
              className='mh-text-link'
              type='button'
              onClick={() => {
                setEndpoint(DEFAULT_ENDPOINT)
                form.reset({ endpoint: '' })
                setSaved(false)
                setCustomModel(null)
              }}
            >
              {t('重置')}
            </button>
          </div>
        </form>
        <div className='mh-field'>
          <label htmlFor='home-request'>{t('请求')}</label>
          <select
            id='home-request'
            value={request}
            onChange={(event) => {
              setRequest(event.target.value as 'models' | 'chat')
              setCopyState('idle')
            }}
          >
            <option value='models'>{t('模型列表')}</option>
            <option value='chat'>{t('对话')}</option>
          </select>
        </div>
        {request === 'chat' && (
          <div className='mh-field'>
            <label htmlFor='home-model-id'>Model ID</label>
            <input
              id='home-model-id'
              value={modelId}
              onChange={(event) => setCustomModel(event.target.value)}
              placeholder='MODEL_ID'
              autoComplete='off'
              spellCheck={false}
            />
          </div>
        )}
        <div className='mh-endpoint-state'>
          <span>{t('状态')}</span>
          <span>{t(endpoint === DEFAULT_ENDPOINT ? '待配置' : '已配置')}</span>
        </div>
      </div>
      <div className='mh-code-workbench'>
        <div className='mh-code-toolbar'>
          <div className='mh-terminal-dots' aria-hidden='true'>
            <span className='mh-dot mh-dot-close' />
            <span className='mh-dot mh-dot-min' />
            <span className='mh-dot mh-dot-max' />
          </div>
          <HomeTabs
            id='code'
            labelKey='marketHome.codeLanguages'
            value={language}
            onChange={(value) => {
              setLanguage(value)
              setCopyState('idle')
            }}
            options={CODE_OPTIONS}
            variant='code'
          />
          <button
            className='mh-copy-button'
            type='button'
            disabled={copyBusy}
            aria-busy={copyBusy}
            onClick={() => void copyCode()}
          >
            {copyState === 'success' ? (
              <Check aria-hidden='true' />
            ) : (
              <Copy aria-hidden='true' />
            )}
            <span aria-live='polite'>{copyLabel()}</span>
          </button>
        </div>
        <div
          className='mh-code-panel'
          id='code-panel'
          role='tabpanel'
          aria-labelledby={`code-${language}`}
          tabIndex={0}
          key={`${language}-${request}`}
        >
          <pre>
            <HighlightedCode source={source} />
          </pre>
        </div>
        <div className='mh-code-footer'>
          <span>
            {request === 'models'
              ? 'GET /v1/models'
              : 'POST /v1/chat/completions'}
          </span>
          <span>{t('示例')}</span>
        </div>
        <details className='mh-response'>
          <summary>
            {t('marketHome.response')}
            <ChevronDown aria-hidden='true' />
          </summary>
          <pre>
            <code>
              {JSON.stringify(
                request === 'models'
                  ? {
                      object: 'list',
                      data: [{ id: 'MODEL_ID', object: 'model' }],
                    }
                  : {
                      object: 'chat.completion',
                      model: modelId || 'MODEL_ID',
                      choices: [
                        { message: { role: 'assistant', content: 'Hello' } },
                      ],
                    },
                null,
                2
              )}
            </code>
          </pre>
        </details>
        {manualOpen && (
          <div className='mh-manual-copy'>
            <label htmlFor='home-manual-code'>
              {t('marketHome.manualTitle')}
            </label>
            <textarea
              ref={manualCode}
              id='home-manual-code'
              value={source}
              readOnly
              spellCheck={false}
              wrap='off'
            />
            <button
              className='mh-text-link'
              type='button'
              onClick={() => {
                manualCode.current?.focus()
                manualCode.current?.select()
              }}
            >
              {t('marketHome.selectCode')}
            </button>
          </div>
        )}
      </div>
    </div>
  )
}
