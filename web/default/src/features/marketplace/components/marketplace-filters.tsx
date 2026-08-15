import {
  Activity,
  ArrowDown,
  ArrowUp,
  Gauge,
  Rabbit,
  Scale,
  Search,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/native-select'
import { MARKETPLACE_SOURCE_OPTIONS } from '../lib/channel-form'
import type { GroupFilters } from '../types'

export function MarketplaceFilters(props: {
  filters: GroupFilters
  onChange: (patch: Partial<GroupFilters>) => void
}) {
  const { t } = useTranslation()
  const sorts = [
    { value: 'score', label: t('综合'), icon: Gauge },
    { value: 'success_rate', label: t('成功率'), icon: ShieldCheck },
    { value: 'ttft', label: t('首字速度'), icon: Rabbit },
    { value: 'multiplier', label: t('倍率'), icon: Scale },
    { value: 'requests', label: t('请求量'), icon: Activity },
  ]

  return (
    <div className='border-border bg-muted/10 border-y'>
      <div className='flex flex-col gap-3 px-4 py-4 xl:flex-row xl:items-center'>
        <label className='relative min-w-0 flex-1 xl:max-w-xl'>
          <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
          <Input
            value={props.filters.search}
            onChange={(event) =>
              props.onChange({ search: event.target.value, page: 1 })
            }
            placeholder={t('搜索渠道 ID、渠道名、模型或来源')}
            className='bg-background pl-9'
          />
        </label>
        <div className='flex flex-wrap items-center gap-2'>
          <Input
            value={props.filters.model}
            onChange={(event) =>
              props.onChange({ model: event.target.value, page: 1 })
            }
            placeholder={t('指定模型')}
            aria-label={t('指定模型')}
            className='bg-background w-36 sm:w-44'
          />
          <NativeSelect
            value={props.filters.source}
            onChange={(event) =>
              props.onChange({ source: event.target.value, page: 1 })
            }
            aria-label={t('模型来源')}
            className='bg-background'
          >
            <option value=''>{t('全部来源')}</option>
            {MARKETPLACE_SOURCE_OPTIONS.map((source) => (
              <option key={source} value={source}>
                {source}
              </option>
            ))}
          </NativeSelect>
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
            <option value='active'>{t('可用')}</option>
            <option value='degraded'>{t('质量下降')}</option>
            <option value='suspended'>{t('已暂停')}</option>
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
      </div>
      <div className='border-border flex items-center gap-2 overflow-x-auto border-t px-4 py-2.5'>
        <span className='text-muted-foreground mr-1 shrink-0 text-xs'>
          {t('排序')}
        </span>
        {sorts.map(({ value, label, icon: Icon }) => {
          const active = props.filters.sort === value
          return (
            <Button
              key={value}
              type='button'
              variant={active ? 'secondary' : 'ghost'}
              size='xs'
              className={cn('shrink-0', active && 'text-primary')}
              onClick={() => props.onChange({ sort: value, page: 1 })}
            >
              <Icon />
              {label}
            </Button>
          )
        })}
        <div className='bg-border mx-1 h-4 w-px shrink-0' />
        <Button
          type='button'
          variant='ghost'
          size='xs'
          className='shrink-0'
          title={props.filters.direction === 'asc' ? t('升序') : t('降序')}
          onClick={() =>
            props.onChange({
              direction: props.filters.direction === 'asc' ? 'desc' : 'asc',
              page: 1,
            })
          }
        >
          {props.filters.direction === 'asc' ? <ArrowUp /> : <ArrowDown />}
          {props.filters.direction === 'asc' ? t('升序') : t('降序')}
        </Button>
      </div>
    </div>
  )
}
