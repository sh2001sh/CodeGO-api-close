import { useMemo, useState } from 'react'
import { getIntlLocale } from '@/i18n/format'
import { ChevronLeft, ChevronRight, ChevronsUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

interface BoundedSelectProps {
  options: readonly { value: string; label: string }[]
  value: string
  onValueChange: (value: string) => void
  placeholder?: string
  className?: string
  disabled?: boolean
  id?: string
  'aria-label'?: string
}

const PAGE_SIZE = 50

export function BoundedSelect({
  options,
  value,
  onValueChange,
  placeholder,
  className,
  disabled,
  id,
  'aria-label': ariaLabel,
}: BoundedSelectProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [requestedPage, setRequestedPage] = useState(1)
  const selected = useMemo(
    () => options.find((option) => option.value === value),
    [options, value]
  )
  const filtered = useMemo(() => {
    const keyword = search.trim().toLocaleLowerCase()
    return keyword
      ? options.filter((option) =>
          `${option.value} ${option.label}`
            .toLocaleLowerCase()
            .includes(keyword)
        )
      : options
  }, [options, search])
  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const page = Math.min(requestedPage, pageCount)
  if (requestedPage !== page) setRequestedPage(page)
  const label = selected?.label || value || placeholder || t('Select')
  const missingSelected = !selected
  const number = new Intl.NumberFormat(getIntlLocale())

  if (options.length + Number(missingSelected) <= PAGE_SIZE) {
    return (
      <NativeSelect
        id={id}
        className={className}
        value={value}
        disabled={disabled}
        aria-label={ariaLabel}
        onChange={(event) => onValueChange(event.target.value)}
      >
        {missingSelected && (
          <NativeSelectOption value={value} disabled>
            {label}
          </NativeSelectOption>
        )}
        {options.map((option) => (
          <NativeSelectOption key={option.value} value={option.value}>
            {option.label}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    )
  }

  return (
    <Popover
      open={open}
      onOpenChange={(nextOpen) => {
        setOpen(nextOpen)
        if (nextOpen) {
          setSearch('')
          const selectedIndex = options.findIndex(
            (option) => option.value === value
          )
          setRequestedPage(
            Math.max(1, Math.floor(selectedIndex / PAGE_SIZE) + 1)
          )
        }
      }}
    >
      <PopoverTrigger
        render={
          <Button
            id={id}
            type='button'
            variant='outline'
            role='combobox'
            aria-expanded={open}
            aria-label={ariaLabel}
            disabled={disabled}
            className={cn('min-w-0 justify-between gap-2', className)}
          />
        }
      >
        <span className='min-w-0 truncate'>{label}</span>
        <ChevronsUpDown className='size-4 shrink-0 opacity-50' aria-hidden />
      </PopoverTrigger>
      <PopoverContent
        align='start'
        className='w-[max(17rem,var(--anchor-width))] max-w-[calc(100vw-2rem)] gap-1 p-1'
      >
        <Command shouldFilter={false}>
          <CommandInput
            aria-label={t('搜索')}
            placeholder={t('搜索')}
            value={search}
            onValueChange={(nextSearch) => {
              setSearch(nextSearch)
              setRequestedPage(1)
            }}
          />
          <CommandList className='max-h-[min(18rem,45dvh)]'>
            <CommandEmpty>{t('No results found.')}</CommandEmpty>
            {filtered
              .slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)
              .map((option) => (
                <CommandItem
                  key={option.value}
                  value={`${option.value.length}:${option.value}`}
                  data-checked={option.value === value}
                  onSelect={() => {
                    onValueChange(option.value)
                    setOpen(false)
                  }}
                >
                  <span className='min-w-0 truncate'>{option.label}</span>
                </CommandItem>
              ))}
          </CommandList>
        </Command>
        <div className='flex items-center justify-between gap-2 px-1 pb-1'>
          <Button
            type='button'
            variant='ghost'
            size='icon'
            aria-label={t('上一页')}
            disabled={page <= 1}
            onClick={() => setRequestedPage(page - 1)}
          >
            <ChevronLeft aria-hidden />
          </Button>
          <span
            className='shrink-0 text-xs tabular-nums'
            role='status'
            aria-label={t('Page {{current}} of {{total}}', {
              current: page,
              total: pageCount,
            })}
          >
            {number.format(page)} / {number.format(pageCount)}
          </span>
          <Input
            type='number'
            min={1}
            max={pageCount}
            value={page}
            aria-label={t('Go to page')}
            className='h-11 w-16 text-center tabular-nums'
            onChange={(event) => {
              const nextPage = Number(event.target.value)
              if (Number.isInteger(nextPage) && nextPage >= 1) {
                setRequestedPage(Math.min(nextPage, pageCount))
              }
            }}
          />
          <Button
            type='button'
            variant='ghost'
            size='icon'
            aria-label={t('下一页')}
            disabled={page >= pageCount}
            onClick={() => setRequestedPage(page + 1)}
          >
            <ChevronRight aria-hidden />
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}
