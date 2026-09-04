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
import { useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import {
  ArrowRight,
  LayoutGrid,
  SearchX,
  Store,
  Table2,
  Waypoints,
  X,
} from 'lucide-react'
import { SiteSeo } from '@/components/seo'
import {
  EXCLUDED_GROUPS,
  QUOTA_TYPE_VALUES,
} from '@/features/pricing/constants'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { mergePricingModels } from '@/features/pricing/lib/merge-pricing-models'
import { formatPrice, formatGroupPrice } from '@/features/pricing/lib/price'
import type { PricingModel, TokenUnit } from '@/features/pricing/types'
import { DawnModal } from '../components/dawn-modal'
import { DawnNav } from '../components/dawn-nav'
import { DawnQueryError } from '../components/query-error'

const VENDOR_RULES: Array<[RegExp, string]> = [
  [/^gpt|^o[134](-mini)?|^chatgpt/, 'OpenAI'],
  [/^claude/, 'Anthropic'],
  [/^gemini/, 'Google'],
  [/^deepseek/, 'DeepSeek'],
  [/^qwen/, 'Alibaba'],
  [/^llama|^meta/, 'Meta'],
  [/^kimi|^moonshot/, 'Moonshot'],
  [/^glm|^chatglm/, 'Zhipu'],
  [/^doubao|^skylark/, 'ByteDance'],
  [/^ernie/, 'Baidu'],
  [/^hunyuan/, 'Tencent'],
  [/^minimax|^abab/, 'MiniMax'],
  [/^yi-/, '01.AI'],
  [/^mixtral|^mistral/, 'Mistral'],
  [/^grok/, 'xAI'],
  [/^command/, 'Cohere'],
  [/^flux|^black-forest/, 'BFL'],
  [/^seedream|^doubao-seed/, 'ByteDance'],
  [/^whisper|^tts|^dall-e|^gpt-image|^sora/, 'OpenAI'],
]

function guessVendor(name: string): string {
  const lower = name.toLowerCase()
  for (const [pattern, vendor] of VENDOR_RULES) {
    if (pattern.test(lower)) return vendor
  }
  return '其它'
}

export function DawnPlaza() {
  const pricing = usePricingData()
  const [unit, setUnit] = useState<TokenUnit>('M')
  const [view, setView] = useState<'table' | 'grid'>('table')
  const [vendor, setVendor] = useState('全部厂家')
  const [keyword, setKeyword] = useState('')
  const [sort, setSort] = useState<'name' | 'price'>('name')
  const [detail, setDetail] = useState<string | null>(null)

  const models = useMemo(
    () =>
      mergePricingModels(pricing.models, pricing.pricedModelDetails)
        .filter((model) => model.model_name)
        .map((model) => ({
          ...model,
          vendorName: model.vendor_name || guessVendor(model.model_name),
        })),
    [pricing.models, pricing.pricedModelDetails]
  )

  const vendors = useMemo(
    () => [
      '全部厂家',
      ...[...new Set(models.map((model) => model.vendorName))].sort(),
    ],
    [models]
  )

  const groups = useMemo(
    () =>
      Object.entries(pricing.usableGroup)
        .filter(([name]) => !EXCLUDED_GROUPS.includes(name))
        .map(([name, meta]) => ({ name, ratio: meta.ratio, desc: meta.desc })),
    [pricing.usableGroup]
  )

  const list = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    const filtered = models.filter(
      (model) =>
        (vendor === '全部厂家' || model.vendorName === vendor) &&
        (!query ||
          model.model_name.toLowerCase().includes(query) ||
          model.vendorName.toLowerCase().includes(query))
    )
    if (sort === 'price') {
      filtered.sort((a, b) => a.model_ratio - b.model_ratio)
    } else {
      filtered.sort((a, b) => a.model_name.localeCompare(b.model_name))
    }
    return filtered
  }, [models, vendor, keyword, sort])

  const metered = models.filter(
    (model) => model.quota_type === QUOTA_TYPE_VALUES.REQUEST
  ).length
  const free = models.filter(
    (model) =>
      model.quota_type === QUOTA_TYPE_VALUES.TOKEN && model.model_ratio === 0
  ).length

  const detailModel = detail
    ? models.find((model) => model.model_name === detail)
    : null
  const detailGroups = useMemo(() => {
    if (!detailModel) return []
    const enabled = Array.isArray(detailModel.enable_groups)
      ? detailModel.enable_groups
      : []
    return groups.filter(
      (group) => !enabled.length || enabled.includes(group.name)
    )
  }, [detailModel, groups])

  return (
    <div className='dawn'>
      <SiteSeo
        title='模型广场 | Code Go'
        description='模型广场 · 输入输出价目'
        canonicalPath='/pricing'
      />
      <DawnNav />
      <main className='dawn-wrap'>
        <div className='mhead'>
          <div>
            <div className='kick'>
              <span className='n'>D·01</span>
              MODEL PLAZA
            </div>
            <h1>
              模型广场，<em>皆有价签</em>。
            </h1>
          </div>
          <div className='seg'>
            <button
              className={unit === 'M' ? 'on' : ''}
              onClick={() => setUnit('M')}
            >
              /1M
            </button>
            <button
              className={unit === 'K' ? 'on' : ''}
              onClick={() => setUnit('K')}
            >
              /1K
            </button>
          </div>
        </div>

        <div className='statband'>
          <div className='cell'>
            <b>{models.length}</b>
            <span>在售模型</span>
          </div>
          <div className='cell'>
            <b>{vendors.length - 1}</b>
            <span>模型厂家</span>
          </div>
          <div className='cell'>
            <b className='c-ok'>{free}</b>
            <span>免费模型</span>
          </div>
          <div className='cell'>
            <b className='c-warn'>{metered}</b>
            <span>按量计费</span>
          </div>
          <div className='cell'>
            <b>{groups.length}</b>
            <span>计费分组</span>
          </div>
        </div>

        <div className='filters'>
          <input
            placeholder='搜索模型 / 厂家'
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
          />
          <div className='seg'>
            {vendors.map((name) => (
              <button
                key={name}
                className={vendor === name ? 'on' : ''}
                onClick={() => setVendor(name)}
              >
                {name}
              </button>
            ))}
          </div>
          <select
            className='fsel'
            value={sort}
            onChange={(event) =>
              setSort(event.target.value as 'name' | 'price')
            }
          >
            <option value='name'>按名称</option>
            <option value='price'>按输入价</option>
          </select>
          <div className='iconseg'>
            <button
              className={view === 'table' ? 'on' : ''}
              onClick={() => setView('table')}
              title='表格'
            >
              <Table2 size={15} />
            </button>
            <button
              className={view === 'grid' ? 'on' : ''}
              onClick={() => setView('grid')}
              title='卡片'
            >
              <LayoutGrid size={15} />
            </button>
          </div>
        </div>

        {pricing.isLoading ? null : pricing.error ? (
          <DawnQueryError
            title='模型价格加载失败'
            description='暂时无法获取模型与计费信息，请稍后重试。'
            onRetry={() => void pricing.refetch()}
          />
        ) : list.length === 0 ? (
          <div className='empty'>
            <span className='eic'>
              <SearchX size={20} />
            </span>
            <b>无匹配模型</b>
          </div>
        ) : view === 'table' ? (
          <div className='gtable' style={{ marginTop: 16 }}>
            <div className='tr th'>
              <span>模型</span>
              <span>输入 /{unit}</span>
              <span>输出 /{unit}</span>
              <span>缓存 写/读</span>
              <span>可用分组</span>
              <span style={{ textAlign: 'right' }}>详情</span>
            </div>
            {list.map((model) => (
              <ModelRow
                key={model.model_name}
                model={model}
                unit={unit}
                onOpen={() => setDetail(model.model_name)}
                groups={groups.map((g) => g.name)}
              />
            ))}
          </div>
        ) : (
          <div className='mgrid'>
            {list.map((model) => (
              <div
                className='mcard'
                key={model.model_name}
                onClick={() => setDetail(model.model_name)}
              >
                <div className='halo' />
                <span
                  className='mn'
                  style={{
                    fontFamily: 'var(--dawn-mono)',
                    fontSize: 12.5,
                    fontWeight: 700,
                    display: 'inline-flex',
                    alignItems: 'center',
                    gap: 9,
                    overflow: 'hidden',
                  }}
                >
                  <span
                    style={{
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    {model.model_name}
                  </span>
                  <span className='ven'>{model.vendorName}</span>
                  {model.quota_type === QUOTA_TYPE_VALUES.REQUEST && (
                    <span className='btag'>按量</span>
                  )}
                </span>
                <div className='pr'>
                  <PriceText model={model} unit={unit} />
                  <span> /{unit} 输入</span>
                </div>
                <div className='sub'>
                  {model.quota_type === QUOTA_TYPE_VALUES.REQUEST
                    ? '按量计费'
                    : `输出 ${formatPrice(model, 'output', unit)} /${unit}`}
                </div>
                <div className='gtags' style={{ marginTop: 12 }}>
                  {groupsFor(
                    model,
                    groups.map((g) => g.name)
                  )
                    .slice(0, 3)
                    .map((name) => (
                      <span className='gtag' key={name}>
                        {name}
                      </span>
                    ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </main>

      <DawnModal
        open={!!detailModel}
        onClose={() => setDetail(null)}
        variant='plain'
        label='模型详情'
      >
        {detailModel && (
          <div className='m-main'>
            <div className='m-head' style={{ marginBottom: 18 }}>
              <h3
                style={{
                  fontFamily: 'var(--dawn-mono)',
                  fontSize: 20,
                  display: 'flex',
                  alignItems: 'center',
                  gap: 10,
                  flexWrap: 'wrap',
                }}
              >
                {detailModel.model_name}
                <span className='ven' style={{ fontSize: 10 }}>
                  {detailModel.vendorName}
                </span>
                {detailModel.quota_type === QUOTA_TYPE_VALUES.REQUEST && (
                  <span className='btag'>按量</span>
                )}
              </h3>
              <button
                className='x'
                onClick={() => setDetail(null)}
                aria-label='关闭'
              >
                <X size={18} />
              </button>
            </div>
            <div className='pgrid'>
              {detailModel.quota_type === QUOTA_TYPE_VALUES.REQUEST ? (
                <>
                  <div className='pc'>
                    <b>{meteredPrice(detailModel)}</b>
                    <span>单价 / 次</span>
                  </div>
                  <div className='pc'>
                    <b className='c-warn' style={{ color: 'var(--dawn-warn)' }}>
                      按量
                    </b>
                    <span>计费方式</span>
                  </div>
                </>
              ) : (
                <>
                  <div
                    className={`pc${detailModel.model_ratio === 0 ? 'free' : ''}`}
                  >
                    <b>{formatPrice(detailModel, 'input', unit)}</b>
                    <span>输入 / {unit}</span>
                  </div>
                  <div
                    className={`pc${detailModel.model_ratio === 0 ? 'free' : ''}`}
                  >
                    <b>{formatPrice(detailModel, 'output', unit)}</b>
                    <span>输出 / {unit}</span>
                  </div>
                  <div className='pc'>
                    <b>{cacheText(detailModel, unit, 'create_cache')}</b>
                    <span>缓存写入 / {unit}</span>
                  </div>
                  <div className='pc free'>
                    <b>{cacheText(detailModel, unit, 'cache')}</b>
                    <span>缓存读取 / {unit}</span>
                  </div>
                </>
              )}
            </div>
            <div className='kvs'>
              <div className='kv2'>
                <b>
                  {detailModel.quota_type === QUOTA_TYPE_VALUES.REQUEST
                    ? '每次调用'
                    : '每 1M tokens'}
                </b>
                计费粒度
              </div>
              <div className='kv2'>
                <b>{detailGroups.length} 个</b>
                覆盖分组
              </div>
              {detailModel.tags && (
                <div className='kv2'>
                  <b>{detailModel.tags}</b>
                  标签
                </div>
              )}
            </div>
            <div className='sect'>
              <Waypoints size={12} />
              各分组价格（含分组倍率）
            </div>
            <div className='gtab'>
              <div className='gr gh'>
                <span>分组</span>
                <span>倍率</span>
                <span>输入 /{unit}</span>
                <span>输出 /{unit}</span>
                <span />
              </div>
              {detailGroups.map((group) => (
                <div className='gr' key={group.name}>
                  <span className='gn'>{group.name}</span>
                  <span className='num'>{group.ratio}×</span>
                  {detailModel.quota_type === QUOTA_TYPE_VALUES.REQUEST ? (
                    <>
                      <span className='num'>
                        <b>{formatPrice(detailModel, 'input', unit)}</b>
                      </span>
                      <span className='num'>按量</span>
                    </>
                  ) : (
                    <>
                      <span className='num'>
                        <b>
                          {formatGroupPrice(
                            detailModel,
                            group.name,
                            'input',
                            unit,
                            false,
                            1,
                            1,
                            pricing.groupRatio
                          )}
                        </b>
                        <span className='u'> /{unit}</span>
                      </span>
                      <span className='num'>
                        <b>
                          {formatGroupPrice(
                            detailModel,
                            group.name,
                            'output',
                            unit,
                            false,
                            1,
                            1,
                            pricing.groupRatio
                          )}
                        </b>
                        <span className='u'> /{unit}</span>
                      </span>
                    </>
                  )}
                  <Link
                    to='/market'
                    style={{
                      color: 'var(--dawn-copper)',
                      fontWeight: 700,
                      fontSize: 11,
                      whiteSpace: 'nowrap',
                    }}
                  >
                    去使用 →
                  </Link>
                </div>
              ))}
            </div>
            <div className='mfoot2'>
              <button className='btn' onClick={() => setDetail(null)}>
                关闭
              </button>
              <Link className='btn primary' to='/market'>
                <Store size={14} />
                去市场选组
              </Link>
            </div>
          </div>
        )}
      </DawnModal>
    </div>
  )
}

function groupsFor(model: PricingModel, allGroups: string[]): string[] {
  const enabled = Array.isArray(model.enable_groups) ? model.enable_groups : []
  if (!enabled.length) return allGroups.slice(0, 3)
  return enabled.filter((name) => allGroups.includes(name))
}

function cacheText(
  model: PricingModel,
  unit: TokenUnit,
  type: 'cache' | 'create_cache'
): string {
  const value = formatPrice(model, type, unit)
  return value.includes('NaN') || value === '-' ? '—' : value
}

function PriceText({ model, unit }: { model: PricingModel; unit: TokenUnit }) {
  if (model.quota_type === QUOTA_TYPE_VALUES.REQUEST) {
    return <>{meteredPrice(model)}</>
  }
  const value = formatPrice(model, 'input', unit)
  const isFree = model.model_ratio === 0
  return (
    <span className={isFree ? 'price free' : undefined}>
      {isFree ? '免费' : value}
    </span>
  )
}

/** 按量计费模型的每次单价。 */
function meteredPrice(model: PricingModel): string {
  const price = model.model_price
  if (price == null || !Number.isFinite(Number(price))) return '—'
  return `$${Number(price) % 1 === 0 ? Number(price).toFixed(0) : Number(price).toFixed(2)}`
}

function ModelRow(props: {
  model: PricingModel & { vendorName: string }
  unit: TokenUnit
  onOpen: () => void
  groups: string[]
}) {
  const { model, unit, onOpen, groups } = props
  const metered = model.quota_type === QUOTA_TYPE_VALUES.REQUEST
  const isFree = !metered && model.model_ratio === 0
  return (
    <div
      className='tr'
      onClick={onOpen}
      role='button'
      tabIndex={0}
      onKeyDown={(event) => {
        if (event.key === 'Enter') onOpen()
      }}
    >
      <span
        className='mn'
        style={{
          fontFamily: 'var(--dawn-mono)',
          fontSize: 12.5,
          fontWeight: 700,
          display: 'flex',
          alignItems: 'center',
          gap: 9,
          overflow: 'hidden',
        }}
      >
        <span
          style={{
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          {model.model_name}
        </span>
        <span className='ven'>{model.vendorName}</span>
        {metered && <span className='btag'>按量</span>}
      </span>
      <span className={`price${isFree ? 'free' : ''}`}>
        <b>
          {metered
            ? meteredPrice(model)
            : isFree
              ? '免费'
              : formatPrice(model, 'input', unit)}
        </b>
        <span className='u'> /{unit}</span>
      </span>
      <span className={`price${isFree ? 'free' : ''}`}>
        <b>
          {metered
            ? '按量'
            : isFree
              ? '免费'
              : formatPrice(model, 'output', unit)}
        </b>
        <span className='u'>{metered ? '' : ` /${unit}`}</span>
      </span>
      <span className='cw'>
        {metered
          ? '—'
          : `${cacheText(model, unit, 'create_cache')} / ${cacheText(model, unit, 'cache')}`}
      </span>
      <span className='gtags'>
        {groupsFor(model, groups)
          .slice(0, 3)
          .map((name) => (
            <span className='gtag' key={name}>
              {name}
            </span>
          ))}
      </span>
      <span
        style={{
          textAlign: 'right',
          color: 'var(--dawn-copper)',
          fontWeight: 700,
          fontSize: 11,
          display: 'inline-flex',
          alignItems: 'center',
          gap: 4,
        }}
      >
        详情 <ArrowRight size={12} />
      </span>
    </div>
  )
}
