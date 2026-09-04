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

For commercial licensing, please contact support@quantumnous.com.
*/
import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  Gauge,
  Link as LinkIcon,
  Package,
  Plus,
  RefreshCw,
  Save,
  Upload,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import { fetchMarketplaceModels } from '@/features/marketplace/api'
import { useMarketplaceMutations } from '@/features/marketplace/hooks'
import { MARKETPLACE_SOURCE_OPTIONS } from '@/features/marketplace/lib/channel-form'
import { DawnModal, ModalHead } from '../components/dawn-modal'

const PROVIDERS = [
  { value: 'openai_compatible', label: 'OpenAI Compatible' },
  { value: 'codex', label: 'Codex' },
  { value: 'azure_openai', label: 'Azure OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'gemini', label: 'Gemini' },
] as const

const VISIBILITIES = [
  { value: 'public', label: '公开参与市场' },
  { value: 'unlisted', label: '邀请可见' },
  { value: 'private', label: '仅自己可见' },
] as const

/** 上架渠道：连接信息 / 模型能力 / 服务策略。 */
export function ChannelFormDialog(props: { open: boolean; onClose: () => void }) {
  const { open, onClose } = props
  const queryClient = useQueryClient()
  const mutations = useMarketplaceMutations()

  const [providerType, setProviderType] = useState<string>('openai_compatible')
  const [baseUrl, setBaseUrl] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [source, setSource] = useState<string>(MARKETPLACE_SOURCE_OPTIONS[0])
  const [models, setModels] = useState<string[]>([])
  const [modelInput, setModelInput] = useState('')
  const [multiplier, setMultiplier] = useState('1')
  const [maxConcurrency, setMaxConcurrency] = useState('10')
  const [userMaxConcurrency, setUserMaxConcurrency] = useState('0')
  const [qps, setQps] = useState('5')
  const [visibility, setVisibility] = useState<'public' | 'unlisted' | 'private'>('public')
  const [maintenance, setMaintenance] = useState('')
  const [sensitiveWords, setSensitiveWords] = useState(true)
  const [fetching, setFetching] = useState(false)
  const [busy, setBusy] = useState(false)

  const syncModels = async () => {
    if (!baseUrl.trim() || !apiKey.trim()) {
      toast.error('填写 API 地址与密钥后同步')
      return
    }
    setFetching(true)
    try {
      const fetched = await fetchMarketplaceModels({
        provider_type: providerType,
        base_url: baseUrl.trim(),
        api_key: apiKey.trim(),
      })
      setModels(fetched)
      toast.success(`同步到 ${fetched.length} 个模型`)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '同步失败')
    } finally {
      setFetching(false)
    }
  }

  const submit = async () => {
    if (!baseUrl.trim().startsWith('https://')) {
      toast.error('API 地址须为 https://')
      return
    }
    if (!apiKey.trim()) {
      toast.error('填写 API 密钥')
      return
    }
    if (!models.length) {
      toast.error('至少声明一个模型')
      return
    }
    setBusy(true)
    try {
      await mutations.create.mutateAsync({
        provider_type: providerType,
        source_label: source,
        base_url: baseUrl.trim(),
        api_key: apiKey.trim(),
        declared_models: models,
        model_prices: {},
        multiplier: Number(multiplier) || 1,
        visibility,
        max_concurrency: Number(maxConcurrency) || 0,
        user_max_concurrency: Number(userMaxConcurrency) || 0,
        qps: Number(qps) || 1,
        maintenance_window: maintenance,
        sensitive_word_interception_enabled: sensitiveWords,
      })
      toast.success('已提交 · 检测通过后自动上架')
      onClose()
      void queryClient.invalidateQueries({ queryKey: ['marketplace-channels'] })
      void queryClient.invalidateQueries({ queryKey: ['marketplace-groups'] })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : '提交失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <DawnModal open={open} onClose={onClose} label='上架渠道'>
      <div className='m-main'>
        <ModalHead title='添加新渠道' onClose={onClose} />
        <div className='msec'>
          <div className='st'>
            <span className='ic'>
              <LinkIcon size={15} />
            </span>
            <b>连接信息</b>
          </div>
          <div className='fr'>
            <div>
              <label className='fl'>协议类型</label>
              <select value={providerType} onChange={(event) => setProviderType(event.target.value)}>
                {PROVIDERS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className='fl'>API 地址</label>
              <input
                placeholder='https://api.example.com'
                value={baseUrl}
                onChange={(event) => setBaseUrl(event.target.value)}
              />
            </div>
            <div>
              <label className='fl'>上游来源</label>
              <select value={source} onChange={(event) => setSource(event.target.value)}>
                {MARKETPLACE_SOURCE_OPTIONS.map((option) => (
                  <option key={option} value={option}>
                    {option}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className='fl'>API 密钥</label>
              <input
                placeholder='sk-…'
                type='password'
                value={apiKey}
                onChange={(event) => setApiKey(event.target.value)}
              />
            </div>
          </div>
        </div>

        <div className='msec'>
          <div className='st'>
            <span className='ic'>
              <Package size={15} />
            </span>
            <b>模型能力</b>
            <span className='stx'>{models.length} 个模型</span>
          </div>
          <div className='fr'>
            <div className='full' style={{ display: 'flex', gap: 8 }}>
              <button className='btn mini' onClick={() => void syncModels()} disabled={fetching}>
                <RefreshCw size={13} className={fetching ? 'animate-spin' : ''} />
                同步上游模型
              </button>
              <input
                placeholder='输入模型名称，例如 gpt-5.2'
                style={{ flex: 1 }}
                value={modelInput}
                onChange={(event) => setModelInput(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && modelInput.trim()) {
                    event.preventDefault()
                    setModels((current) =>
                      current.includes(modelInput.trim()) ? current : [...current, modelInput.trim()]
                    )
                    setModelInput('')
                  }
                }}
              />
              <button
                className='btn mini'
                onClick={() => {
                  if (!modelInput.trim()) return
                  setModels((current) =>
                    current.includes(modelInput.trim()) ? current : [...current, modelInput.trim()]
                  )
                  setModelInput('')
                }}
              >
                <Plus size={13} />
              </button>
            </div>
            <div
              className='full'
              style={{
                border: '1px dashed rgba(184,86,46,.3)',
                borderRadius: 10,
                padding: models.length ? 14 : 24,
                textAlign: models.length ? 'left' : 'center',
                color: 'var(--dawn-ink2)',
                fontSize: 12,
                background: 'var(--dawn-cream)',
              }}
            >
              {models.length ? (
                <div className='mline' style={{ marginTop: 0 }}>
                  {models.map((model) => (
                    <button
                      className='mtag'
                      key={model}
                      title='移除'
                      onClick={() => setModels((current) => current.filter((item) => item !== model))}
                    >
                      {model} <X size={10} />
                    </button>
                  ))}
                </div>
              ) : (
                '模型列表将在这里显示'
              )}
            </div>
          </div>
        </div>

        <div className='msec'>
          <div className='st'>
            <span className='ic'>
              <Gauge size={15} />
            </span>
            <b>服务策略</b>
          </div>
          <div className='fr'>
            <div>
              <label className='fl'>消费倍率</label>
              <input
                type='number'
                step='0.05'
                min='0.1'
                value={multiplier}
                onChange={(event) => setMultiplier(event.target.value)}
              />
            </div>
            <div>
              <label className='fl'>可见性</label>
              <select
                value={visibility}
                onChange={(event) => setVisibility(event.target.value as 'public' | 'unlisted' | 'private')}
              >
                {VISIBILITIES.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className='fl'>渠道总并发（0 不限）</label>
              <input value={maxConcurrency} onChange={(event) => setMaxConcurrency(event.target.value)} />
            </div>
            <div>
              <label className='fl'>单用户并发（0 不限）</label>
              <input
                value={userMaxConcurrency}
                onChange={(event) => setUserMaxConcurrency(event.target.value)}
              />
            </div>
            <div>
              <label className='fl'>QPS</label>
              <input value={qps} onChange={(event) => setQps(event.target.value)} />
            </div>
            <div>
              <label className='fl'>维护窗口</label>
              <input
                placeholder='每周日 02:00-03:00 UTC+8'
                value={maintenance}
                onChange={(event) => setMaintenance(event.target.value)}
              />
            </div>
            <div
              className='full'
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 12,
                background: 'var(--dawn-cream)',
                border: '1px solid var(--dawn-line)',
                borderRadius: 10,
                padding: '11px 14px',
              }}
            >
              <b style={{ fontSize: 12.5 }}>敏感词拦截</b>
              <div
                className={`toggle${sensitiveWords ? '' : ' off'}`}
                role='switch'
                aria-checked={sensitiveWords}
                tabIndex={0}
                onClick={() => setSensitiveWords((current) => !current)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') setSensitiveWords((current) => !current)
                }}
              >
                <i />
              </div>
            </div>
          </div>
        </div>

        <div className='m-foot'>
          <button className='btn' onClick={onClose}>
            取消
          </button>
          <button className='btn primary' disabled={busy} onClick={() => void submit()}>
            <Upload size={14} />
            提交上架
          </button>
        </div>
      </div>
      <div className='m-rail'>
        <h5>
          <Save size={14} />
          市场结算规则
        </h5>
        <div className='rsub'>SETTLEMENT</div>
        <div className='rule'>
          <span className='ric'>
            <Package size={14} />
          </span>
          <div>
            <b>支持套餐与通用余额</b>
          </div>
        </div>
        <div className='rule'>
          <span className='ic' style={{ width: 28, height: 28, borderRadius: 9, background: '#fff', border: '1px solid rgba(184,86,46,.25)', display: 'grid', placeItems: 'center', flex: 'none' }}>
            <Gauge size={14} color='var(--dawn-copper)' />
          </span>
          <div>
            <b>95% 渠道收入</b>
          </div>
        </div>
        <div className='rule'>
          <span className='ric'>
            <RefreshCw size={14} />
          </span>
          <div>
            <b>默认 24 小时释放</b>
          </div>
        </div>
        <div className='rule'>
          <span className='ric'>
            <Upload size={14} />
          </span>
          <div>
            <b>检测通过自动上架</b>
          </div>
        </div>
      </div>
    </DawnModal>
  )
}
