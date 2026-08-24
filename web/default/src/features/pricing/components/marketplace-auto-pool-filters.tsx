import { ListFilter, RotateCcw, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

export type AutoPoolSort =
  | 'route'
  | 'multiplier'
  | 'success'
  | 'cache'
  | 'latency'

export function AutoPoolFilters(props: {
  search: string
  onSearchChange: (value: string) => void
  modelFilter: string
  onModelFilterChange: (value: string) => void
  modelOptions: string[]
  sortBy: AutoPoolSort
  onSortChange: (value: AutoPoolSort) => void
  onReset: () => void
}) {
  const { t } = useTranslation()
  const hasFilters =
    props.search.trim() !== '' ||
    props.modelFilter !== 'all' ||
    props.sortBy !== 'route'
  const sortLabels: Record<AutoPoolSort, string> = {
    route: t('推荐路由顺序'),
    success: t('成功率：高到低'),
    multiplier: t('倍率：低到高'),
    cache: t('缓存命中率：高到低'),
    latency: t('延迟：低到高'),
  }
  return (
    <div className='space-y-3'>
      <FilterHeading showReset={hasFilters} onReset={props.onReset} />
      <div className='grid gap-2 sm:grid-cols-2 xl:grid-cols-[minmax(260px,1.4fr)_minmax(180px,0.8fr)_minmax(180px,0.8fr)]'>
        <LabeledControl label={t('搜索')}>
          <div className='relative'>
            <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
            <Input
              value={props.search}
              onChange={(event) => props.onSearchChange(event.target.value)}
              placeholder={t('分组、来源或模型')}
              aria-label={t('搜索分组、来源或模型')}
              className='h-10 pl-9'
            />
          </div>
        </LabeledControl>
        <LabeledControl label={t('模型')}>
          <PoolSelect
            value={props.modelFilter}
            onChange={props.onModelFilterChange}
            allLabel={t('全部模型')}
            options={props.modelOptions}
          />
        </LabeledControl>
        <LabeledControl label={t('排序方式')}>
          <Select
            value={props.sortBy}
            onValueChange={(value) =>
              props.onSortChange((value || 'route') as AutoPoolSort)
            }
          >
            <SelectTrigger className='h-10 w-full'>
              <SelectValue>{sortLabels[props.sortBy]}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {Object.entries(sortLabels).map(([value, label]) => (
                <SelectItem key={value} value={value}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </LabeledControl>
      </div>
    </div>
  )
}

function FilterHeading(props: { showReset: boolean; onReset: () => void }) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap items-center justify-between gap-2'>
      <div className='flex items-center gap-2'>
        <ListFilter className='text-primary size-4' aria-hidden='true' />
        <span className='text-sm font-medium'>{t('筛选与排序')}</span>
      </div>
      {props.showReset && (
        <Button
          type='button'
          variant='ghost'
          size='sm'
          className='text-muted-foreground h-8 gap-1.5 px-2 text-xs'
          onClick={props.onReset}
        >
          <RotateCcw className='size-3.5' />
          {t('清除筛选')}
        </Button>
      )}
    </div>
  )
}

function LabeledControl(props: { label: string; children: React.ReactNode }) {
  return (
    <label className='min-w-0 space-y-1'>
      <span className='text-muted-foreground block text-[11px] font-medium'>
        {props.label}
      </span>
      {props.children}
    </label>
  )
}

function PoolSelect(props: {
  value: string
  onChange: (value: string) => void
  allLabel: string
  options: string[]
}) {
  return (
    <Select
      value={props.value}
      onValueChange={(value) => props.onChange(value || 'all')}
    >
      <SelectTrigger className='h-10 w-full'>
        <SelectValue>
          {props.value === 'all' ? props.allLabel : props.value}
        </SelectValue>
      </SelectTrigger>
      <SelectContent>
        <SelectItem value='all'>{props.allLabel}</SelectItem>
        {props.options.map((option) => (
          <SelectItem key={option} value={option}>
            {option}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
