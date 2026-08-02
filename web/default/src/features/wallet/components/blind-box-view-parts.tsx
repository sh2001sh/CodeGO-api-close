import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { BlindBoxProp } from '../types'

export function BlindBoxPropsList(props: {
  props: BlindBoxProp[]
  disabled: boolean
  onUse: (prop: BlindBoxProp) => void
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

          return (
            <div
              key={prop.id}
              className='border-border/70 bg-background/60 flex flex-wrap items-center justify-between gap-3 rounded-lg border px-3 py-2.5'
            >
              <div className='min-w-0'>
                <div className='text-foreground truncate text-sm font-medium'>
                  {prop.title}
                </div>
                <div className='text-muted-foreground mt-0.5 text-xs'>
                  {getPropDescription(prop, t)}
                </div>
              </div>
              {manual ? (
                <Button
                  type='button'
                  size='sm'
                  variant={active ? 'secondary' : 'default'}
                  onClick={() => props.onUse(prop)}
                  disabled={props.disabled || !available}
                >
                  {active
                    ? t('Active')
                    : available
                      ? t('Use')
                      : getPropStatusLabel(prop.status, t)}
                </Button>
              ) : (
                <span className='text-muted-foreground text-xs'>
                  {getPropStatusLabel(prop.status, t)}
                </span>
              )}
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
  ].includes(prop.prop_type)
}

function getPropDescription(
  prop: BlindBoxProp,
  t: (key: string, options?: Record<string, unknown>) => string
) {
  if (prop.status === 'active' && prop.expires_at) {
    return t('Active until {{date}}', {
      date: new Date(prop.expires_at * 1000).toLocaleString(),
    })
  }
  if (isManualUseProp(prop)) {
    if (prop.prop_type === 'zero_hour_multiplier') {
      return prop.status === 'available'
        ? '启用后 1 小时内可使用 zero-hour 分组，默认分组非生图模型按 0 倍率计费。'
        : 'zero-hour 分组已激活，仅限当前用户，单用户并发最多 5 个请求。'
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

function getPropStatusLabel(
  status: BlindBoxProp['status'],
  t: (key: string) => string
) {
  switch (status) {
    case 'available':
      return t('Available')
    case 'active':
      return t('Active')
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
