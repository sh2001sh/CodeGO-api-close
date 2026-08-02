import { useState } from 'react'
import { Gift } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { ResourceConfig } from './types'

export function AdminRewardSetting(props: {
  config?: ResourceConfig
  pending: boolean
  onSave: (rewardUsd: number) => void
}) {
  const { t } = useTranslation()
  const [value, setValue] = useState(() =>
    String(props.config?.reward_usd ?? 0)
  )
  const numericValue = Number(value)
  return (
    <section className='flex flex-col gap-4 rounded-lg border p-4 sm:flex-row sm:items-end sm:justify-between'>
      <div className='flex max-w-2xl items-start gap-3'>
        <div className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
          <Gift className='size-4' />
        </div>
        <div>
          <h2 className='text-sm font-semibold'>
            {t('Acknowledgement reward')}
          </h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t(
              'Quota is granted once per GitHub repository after an administrator verifies its shu26.cfd acknowledgement.'
            )}
          </p>
        </div>
      </div>
      <div className='flex items-end gap-2'>
        <div className='space-y-1.5'>
          <Label htmlFor='community-reward-usd'>{t('Reward (USD)')}</Label>
          <Input
            id='community-reward-usd'
            className='w-32'
            type='number'
            min='0'
            max='1000'
            step='0.01'
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
        </div>
        <Button
          disabled={
            props.pending ||
            !Number.isFinite(numericValue) ||
            numericValue < 0 ||
            numericValue > 1000
          }
          onClick={() => props.onSave(numericValue)}
        >
          {props.pending ? t('Saving...') : t('Save')}
        </Button>
      </div>
    </section>
  )
}
