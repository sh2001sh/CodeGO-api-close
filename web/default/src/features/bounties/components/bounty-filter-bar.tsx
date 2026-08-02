import { Search, SlidersHorizontal, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import type { BountySearch } from '../types'

interface BountyFilterBarProps {
  search: BountySearch
  onSearchChange: (next: Partial<BountySearch>) => void
}

export function BountyFilterBar(props: BountyFilterBarProps) {
  const { t } = useTranslation()
  const hasFilter = Boolean(
    props.search.keyword ||
    props.search.tag ||
    (props.search.wallet_type && props.search.wallet_type !== 'all') ||
    (props.search.status && props.search.status !== 'all') ||
    (props.search.sort && props.search.sort !== 'latest') ||
    props.search.min_reward ||
    props.search.max_reward
  )
  return (
    <section
      className='border-border/70 bg-card/55 space-y-3 rounded-xl border p-3'
      aria-label={t('Filter tasks')}
    >
      <div className='flex items-center gap-2 text-sm font-medium'>
        <SlidersHorizontal
          className='text-muted-foreground size-4'
          aria-hidden='true'
        />
        <span>{t('Find a task')}</span>
      </div>
      <div className='grid gap-3 md:grid-cols-[minmax(220px,1fr)_minmax(150px,0.35fr)_minmax(150px,0.35fr)_minmax(150px,0.35fr)]'>
        <div className='relative'>
          <Label htmlFor='bounty-keyword' className='sr-only'>
            {t('Search tasks or technologies')}
          </Label>
          <Search
            className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2'
            aria-hidden='true'
          />
          <Input
            id='bounty-keyword'
            value={props.search.keyword ?? ''}
            onChange={(event) =>
              props.onSearchChange({ keyword: event.target.value, page: 1 })
            }
            placeholder={t('Search tasks or technologies')}
            className='h-9 pl-9'
          />
        </div>
        <div>
          <Label htmlFor='bounty-wallet' className='sr-only'>
            {t('Quota type')}
          </Label>
          <NativeSelect
            id='bounty-wallet'
            value={props.search.wallet_type ?? 'all'}
            onChange={(event) =>
              props.onSearchChange({
                wallet_type: event.target.value as BountySearch['wallet_type'],
                page: 1,
              })
            }
            className='w-full'
          >
            <NativeSelectOption value='all'>
              {t('All quota types')}
            </NativeSelectOption>
            <NativeSelectOption value='wallet'>
              {t('Normal quota')}
            </NativeSelectOption>
            <NativeSelectOption value='claude_wallet'>
              {t('Claude quota')}
            </NativeSelectOption>
          </NativeSelect>
        </div>
        <div>
          <Label htmlFor='bounty-status' className='sr-only'>
            {t('Task status')}
          </Label>
          <NativeSelect
            id='bounty-status'
            value={props.search.status ?? 'all'}
            onChange={(event) =>
              props.onSearchChange({
                status: event.target.value as BountySearch['status'],
                page: 1,
              })
            }
            className='w-full'
          >
            <NativeSelectOption value='all'>
              {t('All statuses')}
            </NativeSelectOption>
            <NativeSelectOption value='available'>
              {t('Available to apply')}
            </NativeSelectOption>
            <NativeSelectOption value='active'>
              {t('In progress')}
            </NativeSelectOption>
            <NativeSelectOption value='ending_soon'>
              {t('Ending soon')}
            </NativeSelectOption>
            <NativeSelectOption value='completed'>
              {t('Completed')}
            </NativeSelectOption>
          </NativeSelect>
        </div>
        <div>
          <Label htmlFor='bounty-sort' className='sr-only'>
            {t('Sort tasks')}
          </Label>
          <NativeSelect
            id='bounty-sort'
            value={props.search.sort ?? 'latest'}
            onChange={(event) =>
              props.onSearchChange({
                sort: event.target.value as BountySearch['sort'],
                page: 1,
              })
            }
            className='w-full'
          >
            <NativeSelectOption value='latest'>
              {t('Newest first')}
            </NativeSelectOption>
            <NativeSelectOption value='reward_desc'>
              {t('Highest reward')}
            </NativeSelectOption>
            <NativeSelectOption value='deadline_asc'>
              {t('Closest deadline')}
            </NativeSelectOption>
          </NativeSelect>
        </div>
      </div>
      <div className='flex flex-wrap items-center gap-2'>
        <Label htmlFor='bounty-tag' className='text-muted-foreground text-xs'>
          {t('Technology tag')}
        </Label>
        <Input
          id='bounty-tag'
          value={props.search.tag ?? ''}
          onChange={(event) =>
            props.onSearchChange({ tag: event.target.value, page: 1 })
          }
          placeholder={t('React, Go, performance')}
          className='h-8 w-52'
        />
        <Label
          htmlFor='bounty-min-reward'
          className='text-muted-foreground text-xs'
        >
          {t('Reward range (USD)')}
        </Label>
        <Input
          id='bounty-min-reward'
          type='number'
          min={0.01}
          step={0.01}
          value={props.search.min_reward ?? ''}
          onChange={(event) =>
            props.onSearchChange({
              min_reward: event.target.value
                ? Number(event.target.value)
                : undefined,
              page: 1,
            })
          }
          placeholder={t('Minimum')}
          className='h-8 w-24'
        />
        <Input
          id='bounty-max-reward'
          type='number'
          min={0.01}
          step={0.01}
          value={props.search.max_reward ?? ''}
          onChange={(event) =>
            props.onSearchChange({
              max_reward: event.target.value
                ? Number(event.target.value)
                : undefined,
              page: 1,
            })
          }
          placeholder={t('Maximum')}
          className='h-8 w-24'
        />
        {hasFilter ? (
          <Button
            variant='ghost'
            size='sm'
            onClick={() =>
              props.onSearchChange({
                keyword: '',
                tag: '',
                wallet_type: 'all',
                status: 'all',
                sort: 'latest',
                min_reward: undefined,
                max_reward: undefined,
                page: 1,
              })
            }
          >
            <X aria-hidden='true' />
            {t('Clear filters')}
          </Button>
        ) : null}
      </div>
    </section>
  )
}
