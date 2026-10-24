import interfaceI18n from '@/i18n/config'
import { getIntlLocale } from '@/i18n/format'
import { CirclePause, CirclePlay, Gift, RefreshCw } from 'lucide-react'
import { Trans, useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { BlindBoxProp } from '../types'

export function BlindBoxPropsList(props: {
  props: BlindBoxProp[]
  disabled: boolean
  onUse: (prop: BlindBoxProp) => void
  onPause: (prop: BlindBoxProp) => void
  onConvert: (prop: BlindBoxProp) => void
  onGift: (prop: BlindBoxProp) => void
  convertingPropId?: number | null
}) {
  const { t } = useTranslation()

  return (
    <section>
      <div className='mt-3 space-y-2'>
        {props.props.map((prop) => {
          const manual = isManualUseProp(prop)
          const available = prop.status === 'available'
          const active = prop.status === 'active'
          const paused = prop.status === 'paused'
          const monthlyPass = prop.prop_type === 'monthly_pass_multiplier'
          const universalPointOne = prop.prop_type === 'consume_discount_10'
          const zeroHour = prop.prop_type === 'zero_hour_multiplier'
          const pausable = monthlyPass || universalPointOne || zeroHour
          const canUse = available || (pausable && paused)
          const convertible =
            available && prop.prop_type === 'subscription_discount_90'

          return (
            <div
              key={prop.id}
              className='bg-muted/55 flex min-w-0 flex-col items-stretch gap-3 rounded-xl px-4 py-4 sm:flex-row sm:items-center sm:justify-between'
            >
              <div className='min-w-0 flex-1'>
                <div className='text-foreground text-sm font-medium break-words'>
                  {getPropTitle(prop)}
                </div>
                <dl className='mt-2 flex flex-wrap gap-x-4 gap-y-1 text-sm'>
                  {getPropFacts(prop, t).map((fact) => (
                    <div key={fact.label} className='flex flex-wrap gap-x-2'>
                      <dt className='text-muted-foreground'>{fact.label}</dt>
                      <dd>{fact.value}</dd>
                    </div>
                  ))}
                </dl>
              </div>
              <div className='flex w-full shrink-0 flex-col gap-2 sm:w-auto sm:flex-row'>
                {available ? (
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    onClick={() => props.onGift(prop)}
                    disabled={props.disabled}
                    className='w-full sm:w-auto'
                  >
                    <Gift className='size-4' data-icon='inline-start' />
                    <Trans i18nKey={'赠送'} />
                  </Button>
                ) : null}
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
                    <Trans i18nKey={'转为九折充值卡'} />
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
                    <Trans i18nKey={'暂停'} />
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
                    {pausable && canUse ? (
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
    'consume_discount_10',
    'zero_hour_multiplier',
    'monthly_pass_multiplier',
  ].includes(prop.prop_type)
}

function getPropFacts(
  prop: BlindBoxProp,
  t: (key: string, options?: Record<string, unknown>) => string
) {
  const facts = [
    { label: t('状态'), value: getPropStatusLabel(prop.status, t) },
  ]
  if (isManualUseProp(prop)) {
    const pausable = [
      'consume_discount_10',
      'monthly_pass_multiplier',
      'zero_hour_multiplier',
    ].includes(prop.prop_type)
    const runningSeconds =
      prop.status === 'active' && prop.expires_at
        ? prop.expires_at - Math.floor(Date.now() / 1000)
        : prop.remaining_seconds || prop.duration_seconds
    if (pausable) {
      facts.push({ label: t('剩余时间'), value: formatSeconds(runningSeconds) })
    } else if (prop.status === 'active' && prop.expires_at) {
      facts.push({
        label: t('有效期'),
        value: new Date(prop.expires_at * 1000).toLocaleString(getIntlLocale()),
      })
    } else if (prop.status === 'available') {
      facts.push({
        label: t('生效时长'),
        value: formatSeconds(prop.duration_seconds),
      })
    }
    let scope = t('仅官方渠道')
    if (prop.prop_type === 'consume_discount_10') scope = t('全部现有官方分组')
    if (prop.prop_type === 'monthly_pass_multiplier')
      scope = `${t('官方分组')} · ${t('月卡额度')}`
    if (prop.prop_type === 'zero_hour_multiplier') scope = t('倍率卡专属分组')
    facts.push({ label: t('适用范围'), value: scope })
    if (prop.prop_type === 'zero_hour_multiplier') {
      facts.push({ label: t('并发'), value: '10' })
      facts.push({ label: t('用户'), value: t('仅限当前用户') })
    }
  } else if (
    prop.prop_type === 'topup_discount_90' &&
    prop.status === 'available'
  ) {
    facts.push({
      label: t('适用范围'),
      value: `${t('套餐购买')} / ${t('余额充值')}`,
    })
    facts.push({ label: t('次数'), value: '1' })
    facts.push({ label: t('优惠上限'), value: '100 credits' })
    facts.push({ label: t('生效方式'), value: t('下次订单自动生效') })
  }
  return facts
}

function getPropTitle(prop: BlindBoxProp) {
  if (prop.prop_type === 'topup_discount_90')
    return interfaceI18n.t('九折充值卡')
  if (prop.prop_type === 'consume_discount_10')
    return interfaceI18n.t('15 分钟 0.1 倍率卡')
  if (prop.prop_type === 'monthly_pass_multiplier')
    return interfaceI18n.t('套餐 0.1 倍率卡')
  if (prop.prop_type === 'subscription_discount_90')
    return interfaceI18n.t('历史套餐折扣卡')
  if (prop.prop_type === 'zero_hour_multiplier')
    return interfaceI18n.t('历史 0 倍率道具')
  return prop.title
}

function getPropActionLabel(
  prop: BlindBoxProp,
  active: boolean,
  available: boolean,
  paused: boolean,
  t: (key: string) => string
) {
  if (prop.prop_type === 'consume_discount_10') {
    if (paused) return interfaceI18n.t('继续启用')
    if (available) return interfaceI18n.t('启用')
  }
  if (prop.prop_type === 'monthly_pass_multiplier') {
    if (paused) return interfaceI18n.t('继续开启')
    if (available) return interfaceI18n.t('开启')
  }
  if (prop.prop_type === 'zero_hour_multiplier') {
    if (paused) return interfaceI18n.t('继续开启')
    if (available) return interfaceI18n.t('开启')
  }
  if (active) return t('Active')
  if (available) return t('Use')
  return getPropStatusLabel(prop.status, t)
}

function formatSeconds(value: number) {
  const total = Math.max(0, Math.ceil(value))
  const minutes = Math.floor(total / 60)
  const seconds = total % 60
  if (minutes === 0)
    return interfaceI18n.t('{{param0}} 秒', { param0: seconds })
  return interfaceI18n.t('{{param0}} 分{{param1}}', {
    param0: minutes,
    param1: seconds > 0 ? ` ${seconds} 秒` : '',
  })
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
      return interfaceI18n.t('已暂停')
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
