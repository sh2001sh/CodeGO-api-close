import { CirclePause, CirclePlay, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { BlindBoxProp } from '../types'

export function BlindBoxPropsList(props: {
  props: BlindBoxProp[]
  disabled: boolean
  onUse: (prop: BlindBoxProp) => void
  onPause: (prop: BlindBoxProp) => void
  onConvert: (prop: BlindBoxProp) => void
  convertingPropId?: number | null
}) {
  const { t } = useTranslation()

  return (
    <section className='app-subtle-panel p-4'>
      <div className='text-foreground text-sm font-semibold'>
        {t('My props')}
      </div>
      <div className='text-muted-foreground mt-1 text-xs leading-5'>
        {t(
          'Use available multiplier cards here. Recharge and plan discount cards apply automatically to the next eligible order.'
        )}
      </div>
      <div className='mt-3 space-y-2'>
        {props.props.map((prop) => {
          const manual = isManualUseProp(prop)
          const available = prop.status === 'available'
          const active = prop.status === 'active'
          const paused = prop.status === 'paused'
          const monthlyPass = prop.prop_type === 'monthly_pass_multiplier'
          const zeroHour = prop.prop_type === 'zero_hour_multiplier'
          const pausable = monthlyPass || zeroHour
          const canUse = available || (pausable && paused)
          const convertible =
            available &&
            ['topup_discount_90', 'subscription_discount_90'].includes(
              prop.prop_type
            )

          return (
            <div
              key={prop.id}
              className='border-border/70 bg-background/60 flex min-w-0 flex-col items-stretch gap-2 rounded-lg border px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between sm:gap-3'
            >
              <div className='min-w-0 flex-1'>
                <div className='text-foreground text-sm font-medium break-words'>
                  {prop.title}
                </div>
                <div className='text-muted-foreground mt-0.5 text-xs leading-5 break-words'>
                  {getPropDescription(prop, t)}
                </div>
              </div>
              <div className='flex w-full shrink-0 flex-col gap-2 sm:w-auto sm:flex-row'>
                {convertible ? (
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    onClick={() => props.onConvert(prop)}
                    disabled={
                      props.disabled || props.convertingPropId === prop.id
                    }
                    className='w-full sm:w-auto'
                  >
                    <RefreshCw
                      className={
                        props.convertingPropId === prop.id
                          ? 'size-4 animate-spin motion-reduce:animate-none'
                          : 'size-4'
                      }
                      data-icon='inline-start'
                    />
                    {prop.prop_type === 'topup_discount_90'
                      ? '转为套餐九折卡'
                      : '转为充值九折卡'}
                  </Button>
                ) : null}
                {pausable && active ? (
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    onClick={() => props.onPause(prop)}
                    disabled={props.disabled}
                    className='w-full shrink-0 sm:w-auto'
                  >
                    <CirclePause className='size-4' data-icon='inline-start' />
                    暂停
                  </Button>
                ) : manual ? (
                  <Button
                    type='button'
                    size='sm'
                    variant={active ? 'secondary' : 'default'}
                    onClick={() => props.onUse(prop)}
                    disabled={props.disabled || !canUse}
                    className='w-full shrink-0 sm:w-auto'
                  >
                    {monthlyPass && canUse ? (
                      <CirclePlay className='size-4' data-icon='inline-start' />
                    ) : null}
                    {getPropActionLabel(prop, active, available, paused, t)}
                  </Button>
                ) : (
                  <span className='text-muted-foreground text-xs'>
                    {getPropStatusLabel(prop.status, t)}
                  </span>
                )}
              </div>
            </div>
          )
        })}
      </div>
    </section>
  )
}
function isManualUseProp(prop: BlindBoxProp) {
  return [
    'consume_discount_95',
    'consume_discount_90',
    'zero_hour_multiplier',
    'monthly_pass_multiplier',
  ].includes(prop.prop_type)
}

function getPropDescription(
  prop: BlindBoxProp,
  t: (key: string, options?: Record<string, unknown>) => string
) {
  if (prop.status === 'active' && prop.expires_at) {
    if (prop.prop_type === 'monthly_pass_multiplier') {
      return `0.1 倍率已生效，已接入倍率卡专属分组；剩余约 ${formatSeconds(prop.expires_at - Math.floor(Date.now() / 1000))}，非生图模型可用。`
    }
    if (prop.prop_type === 'zero_hour_multiplier') {
      return `0 倍率已生效，剩余约 ${formatSeconds(prop.expires_at - Math.floor(Date.now() / 1000))}；可随时暂停并保留剩余时间。`
    }
    return t('Active until {{date}}', {
      date: new Date(prop.expires_at * 1000).toLocaleString(),
    })
  }
  if (isManualUseProp(prop)) {
    if (prop.prop_type === 'monthly_pass_multiplier') {
      const remaining = prop.remaining_seconds || prop.duration_seconds
      if (prop.status === 'paused') {
        return `已暂停，剩余 ${formatSeconds(remaining)}。恢复后接入倍率卡专属分组，按 0.1 倍率计费。`
      }
      return `启用后可随时暂停，累计可用 ${formatSeconds(remaining)}；仅限倍率卡专属分组非生图模型。`
    }
    if (prop.prop_type === 'zero_hour_multiplier') {
      const remaining = prop.remaining_seconds || prop.duration_seconds
      if (prop.status === 'paused') {
        return `已暂停，剩余 ${formatSeconds(remaining)}。恢复后重新接入倍率卡专属分组，按 0 倍率计费。`
      }
      return prop.status === 'available'
        ? '启用后可随时暂停，累计可用 1 小时；倍率卡专属分组非生图模型按 0 倍率计费。'
        : '0 倍率已生效，可随时暂停；仅限当前用户，单用户并发最多 10 个请求。'
    }
    return prop.status === 'available'
      ? t('Click Use to activate this card for {{hours}} hours.', {
          hours: Math.max(1, Math.round(prop.duration_seconds / 3600)),
        })
      : t('Multiplier card')
  }
  if (prop.status === 'available') {
    return t('Automatically applied to the next eligible order.')
  }
  return t('This prop is no longer available.')
}

function getPropActionLabel(
  prop: BlindBoxProp,
  active: boolean,
  available: boolean,
  paused: boolean,
  t: (key: string) => string
) {
  if (prop.prop_type === 'monthly_pass_multiplier') {
    if (paused) return '继续开启'
    if (available) return '开启'
  }
  if (prop.prop_type === 'zero_hour_multiplier') {
    if (paused) return '继续开启'
    if (available) return '开启'
  }
  if (active) return t('Active')
  if (available) return t('Use')
  return getPropStatusLabel(prop.status, t)
}

function formatSeconds(value: number) {
  const total = Math.max(0, Math.ceil(value))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  if (minutes === 0) return `${seconds} 秒`
  return `${minutes} 分${seconds > 0 ? ` ${seconds} 秒` : ''}`
}

function getPropStatusLabel(
  status: BlindBoxProp['status'],
  t: (key: string) => string
) {
  switch (status) {
    case 'available':
      return t('Available')
    case 'active':
      return t('Active')
    case 'paused':
      return '已暂停'
    case 'reserved':
      return t('Reserved')
    case 'used':
      return t('Used')
    case 'expired':
      return t('Expired')
    default:
      return status
  }
}
