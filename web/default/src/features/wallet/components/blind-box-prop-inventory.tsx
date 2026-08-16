import { Link } from '@tanstack/react-router'
import { BadgeCheck, Box, Timer } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import type { BlindBoxProp } from '../types'

type PropPresentation = {
  description: string
  actionLabel?: string
  actionTo?: '/wallet' | '/packages'
  manualActivation?: boolean
}

function getPropPresentation(prop: BlindBoxProp): PropPresentation {
  switch (prop.prop_type) {
    case 'topup_discount_90':
      return {
        description: '下次充值时自动按 9 折结算，仅使用一次。',
        actionLabel: '去充值',
        actionTo: '/wallet',
      }
    case 'subscription_discount_90':
      return {
        description: '迁移前获得的历史折扣卡，可在盲盒道具页转换为充值九折卡。',
      }
    case 'consume_discount_95':
      return {
        description: '仅官方渠道可用；启用后连续 24 小时按 0.95 倍率扣减。',
        actionLabel: '立即启用',
        manualActivation: true,
      }
    case 'consume_discount_90':
      return {
        description: '仅官方渠道可用；启用后连续 24 小时按 0.90 倍率扣减。',
        actionLabel: '立即启用',
        manualActivation: true,
      }
    case 'consume_discount_10':
      return {
        description:
          '全部现有官方分组通用，无需切换专属分组；累计 15 分钟，可暂停。',
        actionLabel: '立即启用',
        manualActivation: true,
      }
    case 'monthly_pass_multiplier':
      return {
        description:
          '套餐赠送权益，无需切换分组；仅在实际扣月卡额度时额外乘 0.1，可暂停。',
        actionLabel: '立即启用',
        manualActivation: true,
      }
    default:
      return { description: '该道具将在满足使用条件时自动生效。' }
  }
}

function getStatusPresentation(prop: BlindBoxProp) {
  switch (prop.status) {
    case 'available':
      return {
        label: prop.duration_seconds > 0 ? '待启用' : '待使用',
        className:
          'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300',
      }
    case 'active':
      return {
        label: '生效中',
        className:
          'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
      }
    case 'reserved':
      return {
        label: '已锁定',
        className:
          'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300',
      }
    case 'used':
      return {
        label: '已使用',
        className: 'border-border/70 bg-muted/50 text-muted-foreground',
      }
    default:
      return {
        label: '已失效',
        className: 'border-border/70 bg-muted/50 text-muted-foreground',
      }
  }
}

function formatExpiration(prop: BlindBoxProp) {
  if (prop.status !== 'active' || !prop.expires_at) return null
  return `有效至 ${new Date(prop.expires_at * 1000).toLocaleString()}`
}

export function BlindBoxPropInventory(props: {
  props: BlindBoxProp[]
  onActivate: (prop: BlindBoxProp) => void
}) {
  const activeProps = props.props.filter(
    (prop) => !['used', 'expired'].includes(prop.status)
  )

  if (activeProps.length === 0) return null

  return (
    <section
      className='app-subtle-panel p-4'
      aria-labelledby='blind-box-props-title'
    >
      <div className='mb-3 flex items-center gap-2'>
        <Box className='text-muted-foreground size-4' />
        <div
          id='blind-box-props-title'
          className='text-foreground text-sm font-semibold'
        >
          我的道具
        </div>
        <span className='text-muted-foreground ml-auto text-xs tabular-nums'>
          {activeProps.length} 件可用
        </span>
      </div>

      <div className='space-y-2.5'>
        {activeProps.map((prop) => {
          const presentation = getPropPresentation(prop)
          const status = getStatusPresentation(prop)
          const expiration = formatExpiration(prop)
          const pausable = [
            'monthly_pass_multiplier',
            'consume_discount_10',
            'zero_hour_multiplier',
          ].includes(prop.prop_type)
          const canActivate =
            presentation.manualActivation &&
            (prop.status === 'available' ||
              (pausable && prop.status === 'paused'))
          const canNavigate =
            prop.status === 'available' && presentation.actionTo

          return (
            <div
              key={prop.id}
              className='border-border/70 bg-background/60 rounded-xl border p-3'
            >
              <div className='flex items-start justify-between gap-2'>
                <div className='text-foreground min-w-0 text-sm font-medium'>
                  {getPropTitle(prop)}
                </div>
                <span
                  className={cn(
                    'shrink-0 rounded-full border px-2 py-0.5 text-[11px] font-medium',
                    status.className
                  )}
                >
                  {status.label}
                </span>
              </div>
              <p className='text-muted-foreground mt-1.5 text-xs leading-5'>
                {presentation.description}
              </p>
              {expiration ? (
                <div className='text-muted-foreground mt-2 flex items-center gap-1 text-[11px] tabular-nums'>
                  <Timer className='size-3' />
                  {expiration}
                </div>
              ) : null}
              {prop.status === 'reserved' ? (
                <p className='text-muted-foreground mt-2 text-[11px] leading-4'>
                  已绑定待支付订单，订单完成后会自动使用；超时后会恢复为待使用。
                </p>
              ) : null}
              {canActivate ? (
                <Button
                  type='button'
                  size='sm'
                  className='mt-3 w-full'
                  onClick={() => props.onActivate(prop)}
                >
                  <BadgeCheck className='size-4' data-icon='inline-start' />
                  {presentation.actionLabel}
                </Button>
              ) : null}
              {canNavigate &&
              presentation.actionLabel &&
              presentation.actionTo ? (
                <Button
                  type='button'
                  size='sm'
                  variant='outline'
                  className='mt-3 w-full'
                  render={<Link to={presentation.actionTo} />}
                >
                  {presentation.actionLabel}
                </Button>
              ) : null}
            </div>
          )
        })}
      </div>
    </section>
  )
}

function getPropTitle(prop: BlindBoxProp) {
  if (prop.prop_type === 'consume_discount_10') return '15 分钟 0.1 倍率卡'
  if (prop.prop_type === 'monthly_pass_multiplier') return '套餐 0.1 倍率卡'
  if (prop.prop_type === 'subscription_discount_90') return '历史套餐折扣卡'
  if (prop.prop_type === 'zero_hour_multiplier') return '历史 0 倍率道具'
  return prop.title
}
