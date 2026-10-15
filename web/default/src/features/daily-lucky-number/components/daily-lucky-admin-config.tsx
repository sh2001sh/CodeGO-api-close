import { Clock3, Save } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import type { DailyLuckyConfig } from '../types'

export function DailyLuckyAdminConfig(props: {
  value: DailyLuckyConfig
  saving: boolean
  onChange: (patch: Partial<DailyLuckyConfig>) => void
  onSave: () => void
}) {
  const { t } = useTranslation()

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='border-border/70 flex flex-wrap items-start justify-between gap-3 border-b px-4 py-4 sm:px-5'>
        <div className='flex items-start gap-3'>
          <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
            <Clock3 className='size-4' aria-hidden='true' />
          </span>
          <div>
            <h2 className='text-foreground text-base font-semibold'>
              {t('Next draw configuration')}
            </h2>
            <p className='text-muted-foreground mt-1 text-sm leading-5'>
              {t('Changes apply to the next draw. Existing draw snapshots remain immutable.')}
            </p>
          </div>
        </div>
        <Button
          size='sm'
          type='button'
          onClick={props.onSave}
          disabled={props.saving}
        >
          <Save data-icon='inline-start' />
          {props.saving ? t('Saving...') : t('Save configuration')}
        </Button>
      </div>

      <form
        className='space-y-6 p-4 sm:p-5'
        onSubmit={(event) => {
          event.preventDefault()
          props.onSave()
        }}
      >
        <div className='flex items-center justify-between gap-4'>
          <div>
            <Label htmlFor='daily-lucky-enabled'>{t('Activity enabled')}</Label>
            <p className='text-muted-foreground mt-1 text-xs leading-5'>
              {t('Pausing the activity does not discard completed draws or pending settlements.')}
            </p>
          </div>
          <Switch
            id='daily-lucky-enabled'
            checked={props.value.enabled}
            onCheckedChange={(checked) => props.onChange({ enabled: checked })}
            aria-label={t('Activity enabled')}
          />
        </div>

        <ConfigSection title={t('Schedule')}>
          <div className='grid gap-3 sm:grid-cols-3'>
            <ConfigField
              id='daily-lucky-timezone'
              label={t('Timezone')}
              value={props.value.timezone}
              onChange={(value) => props.onChange({ timezone: value })}
            />
            <ConfigField
              id='daily-lucky-hour'
              label={t('Draw hour')}
              type='number'
              min={0}
              max={23}
              value={props.value.draw_hour}
              onChange={(value) => props.onChange({ draw_hour: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-minute'
              label={t('Draw minute')}
              type='number'
              min={0}
              max={59}
              value={props.value.draw_minute}
              onChange={(value) => props.onChange({ draw_minute: Number(value) })}
            />
          </div>
        </ConfigSection>

        <ConfigSection title={t('Base rewards (unified credits)')}>
          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
            <ConfigField
              id='daily-lucky-reward-1'
              label={t('1-digit match')}
              type='number'
              min={0.01}
              step={0.01}
              value={props.value.base_reward_1_usd}
              onChange={(value) => props.onChange({ base_reward_1_usd: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-reward-2'
              label={t('2-digit match')}
              type='number'
              min={0.01}
              step={0.01}
              value={props.value.base_reward_2_usd}
              onChange={(value) => props.onChange({ base_reward_2_usd: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-reward-3'
              label={t('3-digit match')}
              type='number'
              min={0.01}
              step={0.01}
              value={props.value.base_reward_3_usd}
              onChange={(value) => props.onChange({ base_reward_3_usd: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-reward-4'
              label={t('4-digit match')}
              type='number'
              min={0.01}
              step={0.01}
              value={props.value.base_reward_4_usd}
              onChange={(value) => props.onChange({ base_reward_4_usd: Number(value) })}
            />
          </div>
        </ConfigSection>

        <ConfigSection title={t('Tier multipliers')}>
          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-4'>
            <ConfigField
              id='daily-lucky-multiplier-lite'
              label={t('Lite')}
              type='number'
              min={0.01}
              step={0.1}
              value={props.value.multiplier_lite}
              onChange={(value) => props.onChange({ multiplier_lite: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-multiplier-standard'
              label={t('Standard')}
              type='number'
              min={0.01}
              step={0.1}
              value={props.value.multiplier_standard}
              onChange={(value) => props.onChange({ multiplier_standard: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-multiplier-pro'
              label={t('Pro')}
              type='number'
              min={0.01}
              step={0.1}
              value={props.value.multiplier_pro}
              onChange={(value) => props.onChange({ multiplier_pro: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-multiplier-ultra'
              label={t('Ultra')}
              type='number'
              min={0.01}
              step={0.1}
              value={props.value.multiplier_ultra}
              onChange={(value) => props.onChange({ multiplier_ultra: Number(value) })}
            />
          </div>
        </ConfigSection>

        <ConfigSection title={t('Jackpot and budget')}>
          <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-5'>
            <ConfigField
              id='daily-lucky-jackpot-initial'
              label={t('Initial jackpot (unified credits)')}
              type='number'
              min={0.01}
              step={0.01}
              value={props.value.jackpot_initial_usd}
              onChange={(value) => props.onChange({ jackpot_initial_usd: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-jackpot-increment'
              label={t('Daily jackpot increment (unified credits)')}
              type='number'
              min={0}
              step={0.01}
              value={props.value.jackpot_increment_usd}
              onChange={(value) => props.onChange({ jackpot_increment_usd: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-jackpot-cap'
              label={t('Jackpot cap (unified credits)')}
              type='number'
              min={0.01}
              step={0.01}
              value={props.value.jackpot_cap_usd}
              onChange={(value) => props.onChange({ jackpot_cap_usd: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-cost-per-usd'
              label={t('Cost per unified credit (CNY)')}
              type='number'
              min={0}
              step={0.01}
              value={props.value.cost_per_usd}
              onChange={(value) => props.onChange({ cost_per_usd: Number(value) })}
            />
            <ConfigField
              id='daily-lucky-monthly-budget'
              label={t('Monthly budget (unified credits)')}
              type='number'
              min={0}
              step={0.01}
              value={props.value.monthly_budget_usd}
              onChange={(value) => props.onChange({ monthly_budget_usd: Number(value) })}
            />
          </div>
        </ConfigSection>
      </form>
    </section>
  )
}

function ConfigSection(props: { title: string; children: ReactNode }) {
  return (
    <div className='border-border/70 space-y-3 border-t pt-5'>
      <h3 className='text-foreground text-sm font-semibold'>{props.title}</h3>
      {props.children}
    </div>
  )
}

function ConfigField(props: {
  id: string
  label: string
  value: string | number
  onChange: (value: string) => void
  type?: 'text' | 'number'
  min?: number
  max?: number
  step?: number
}) {
  return (
    <div className='space-y-1.5'>
      <Label htmlFor={props.id} className='text-muted-foreground text-xs'>
        {props.label}
      </Label>
      <Input
        id={props.id}
        type={props.type ?? 'text'}
        value={props.value}
        min={props.min}
        max={props.max}
        step={props.step}
        onChange={(event) => props.onChange(event.target.value)}
      />
    </div>
  )
}
