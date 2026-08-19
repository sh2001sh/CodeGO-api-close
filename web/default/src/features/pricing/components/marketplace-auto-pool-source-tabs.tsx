import { useTranslation } from 'react-i18next'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

export interface AutoPoolSourceOption {
  value: string
  label: string
  count: number
}

/** Source navigation for browsing Auto route-pool candidates. */
export function AutoPoolSourceTabs(props: {
  value: string
  options: AutoPoolSourceOption[]
  onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const total = props.options.reduce((sum, option) => sum + option.count, 0)

  return (
    <Tabs value={props.value} onValueChange={props.onChange}>
      <TabsList
        variant='line'
        aria-label={t('按来源查看待选择分组')}
        className='h-10 max-w-full justify-start overflow-x-auto overflow-y-hidden border-b'
      >
        <SourceTab value='all' label={t('全部')} count={total} />
        {props.options.map((option) => (
          <SourceTab key={option.value} {...option} />
        ))}
      </TabsList>
    </Tabs>
  )
}

function SourceTab(props: { value: string; label: string; count: number }) {
  return (
    <TabsTrigger
      value={props.value}
      className='h-9 shrink-0 gap-2 px-3 after:bottom-0'
    >
      <span>{props.label}</span>
      <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-[10px] font-semibold tabular-nums'>
        {props.count}
      </span>
    </TabsTrigger>
  )
}
