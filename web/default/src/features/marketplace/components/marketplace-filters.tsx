import { useState } from 'react'
import {
  Activity,
  ArrowDown,
  ArrowUp,
  ChevronDown,
  Gauge,
  Rabbit,
  Scale,
  Search,
  ShieldCheck,
  SlidersHorizontal,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import type { GroupFilters } from '../types'

export function MarketplaceFilters(props: {
  filters: GroupFilters
  onChange: (patch: Partial<GroupFilters>) => void
}) {
  const { t } = useTranslation()
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const sorts = [
    { value: 'score', label: t('综合'), icon: Gauge },
    { value: 'success_rate', label: t('成功率'), icon: ShieldCheck },
    { value: 'ttft', label: t('首字速度'), icon: Rabbit },
    { value: 'multiplier', label: t('倍率'), icon: Scale },
    { value: 'requests', label: t('请求量'), icon: Activity },
  ]

  return (
    <div className='border-border bg-muted/10 border-b'>
      <div className='flex flex-col gap-2.5 px-4 py-3 sm:px-5 lg:flex-row lg:items-center'>
        <label className='relative min-w-0 flex-1 lg:max-w-xl'>
          <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
          <Input
            value={props.filters.search}
            onChange={(event) =>
              props.onChange({ search: event.target.value, page: 1 })
            }
            placeholder={t('搜索分组、渠道 ID、模型或来源')}
            className='bg-background pl-9'
          />
        </label>
        <div className='flex min-w-0 flex-1 items-center gap-2 lg:justify-end'>
          <Input
            value={props.filters.model}
            onChange={(event) =>
              props.onChange({ model: event.target.value, page: 1 })
            }
            placeholder={t('指定模型')}
            aria-label={t('指定模型')}
            className='bg-background min-w-0 flex-1 sm:max-w-44'
          />
          <SortControls
            sort={props.filters.sort}
            direction={props.filters.direction}
            sorts={sorts}
            onChange={props.onChange}
          />
          <Button
            type='button'
            variant='ghost'
            size='icon-sm'
            className='shrink-0 sm:w-auto sm:px-3'
            onClick={() => setAdvancedOpen((current) => !current)}
            aria-expanded={advancedOpen}
          >
            <SlidersHorizontal className='sm:hidden' />
            <span className='sr-only sm:not-sr-only'>{t('高级筛选')}</span>
            <ChevronDown
              className={cn(
                'hidden transition-transform sm:block',
                advancedOpen && 'rotate-180'
              )}
            />
          </Button>
        </div>
      </div>
      {advancedOpen && (
        <div className='border-border flex flex-wrap items-center gap-2 border-t px-4 py-3 sm:px-5'>
          <NativeSelect
            value={props.filters.provider}
            onChange={(event) =>
              props.onChange({ provider: event.target.value, page: 1 })
            }
            aria-label={t('协议类型')}
            className='bg-background'
          >
            <option value=''>{t('全部协议')}</option>
            <option value='openai_compatible'>OpenAI Compatible</option>
            <option value='codex'>Codex</option>
            <option value='azure_openai'>Azure OpenAI</option>
            <option value='anthropic'>Anthropic / Claude</option>
            <option value='gemini'>Google Gemini</option>
          </NativeSelect>
          <NativeSelect
            value={props.filters.status}
            onChange={(event) =>
              props.onChange({ status: event.target.value, page: 1 })
            }
            aria-label={t('状态')}
            className='bg-background'
          >
            <option value=''>{t('全部状态')}</option>
            <option value='draft'>{t('草稿')}</option>
            <option value='verifying'>{t('检测中')}</option>
            <option value='pending_review'>{t('待审核')}</option>
            <option value='active'>{t('可用')}</option>
            <option value='degraded'>{t('质量下降')}</option>
            <option value='suspended'>{t('已暂停')}</option>
            <option value='disabled'>{t('已停用')}</option>
          </NativeSelect>
          <NativeSelect
            value={props.filters.verification}
            onChange={(event) =>
              props.onChange({ verification: event.target.value, page: 1 })
            }
            aria-label={t('检测状态')}
            className='bg-background'
          >
            <option value=''>{t('全部检测状态')}</option>
            <option value='passed'>{t('检测通过')}</option>
            <option value='queued'>{t('等待检测')}</option>
            <option value='running'>{t('检测中')}</option>
            <option value='failed'>{t('检测未通过')}</option>
          </NativeSelect>
          <NativeSelect
            value={String(props.filters.window_hours)}
            onChange={(event) =>
              props.onChange({
                window_hours: Number(event.target.value),
                page: 1,
              })
            }
            aria-label={t('时间窗口')}
            className='bg-background'
          >
            <option value='24'>{t('近 24 小时')}</option>
            <option value='168'>{t('近 7 天')}</option>
            <option value='720'>{t('近 30 天')}</option>
          </NativeSelect>
        </div>
      )}
    </div>
  )
}

function SortControls(props: {
  sort: string
  direction: string
  sorts: Array<{ value: string; label: string; icon: typeof Gauge }>
  onChange: (patch: Partial<GroupFilters>) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='flex shrink-0 items-center gap-1'>
      <NativeSelect
        value={props.sort}
        onChange={(event) => {
          const sort = event.target.value
          props.onChange({
            sort,
            direction: defaultDirectionForSort(sort),
            page: 1,
          })
        }}
        aria-label={t('排序方式')}
        className='bg-background w-28'
      >
        {props.sorts.map(({ value, label }) => (
          <option key={value} value={value}>
            {t('{{label}}优先', { label })}
          </option>
        ))}
      </NativeSelect>
      <Button
        type='button'
        variant='outline'
        size='icon-sm'
        className='shrink-0'
        title={props.direction === 'asc' ? t('升序') : t('降序')}
        onClick={() =>
          props.onChange({
            direction: props.direction === 'asc' ? 'desc' : 'asc',
            page: 1,
          })
        }
      >
        {props.direction === 'asc' ? <ArrowUp /> : <ArrowDown />}
      </Button>
    </div>
  )
}

function defaultDirectionForSort(sort: string) {
  return sort === 'ttft' || sort === 'multiplier' ? 'asc' : 'desc'
}
