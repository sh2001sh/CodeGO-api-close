/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useState, type ReactNode } from 'react'
import { Boxes, Gift, Loader2, PackageOpen, Sparkles } from 'lucide-react'
import { motion, useReducedMotion } from 'motion/react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { PaymentMethod } from '../types'
import { getBlindBoxMethodLabel } from './blind-box-dialogs'

const EASE_OUT_QUINT = [0.22, 1, 0.36, 1] as const

interface BlindBoxStageProps {
  enabled: boolean
  loading: boolean
  unitPrice: number
  countOptions: number[]
  quantity: number
  payMethods: PaymentMethod[]
  paymentMethod: PaymentMethod | null
  amountDue: number
  paying: boolean
  availableBoxes: number
  openingCount: number | null
  propCount: number
  onQuantityChange: (value: number) => void
  onPaymentMethodChange: (method: PaymentMethod) => void
  onPay: () => void
  onManualOpen: (count: number) => void
  onOpenProps: () => void
}

export function BlindBoxStage(props: BlindBoxStageProps) {
  const reduced = Boolean(useReducedMotion())
  const hasPending = props.availableBoxes > 0
  const opening = props.openingCount !== null
  const disabled = !props.enabled || props.loading
  const [openMode, setOpenMode] = useState<'single' | 'all'>('single')
  const openCount = openMode === 'single' ? 1 : props.availableBoxes

  return (
    <section className='app-page-shell overflow-hidden'>
      <div className='from-primary/[0.07] via-primary/[0.02] relative bg-gradient-to-b to-transparent px-4 pt-7 pb-6 sm:px-6'>
        <AmbientGlow reduced={reduced} />

        <div className='relative flex flex-col items-center gap-5'>
          <BoxFigure
            reduced={reduced}
            pending={hasPending}
            opening={opening}
            onOpen={
              hasPending && !opening
                ? () => props.onManualOpen(openCount)
                : undefined
            }
          />

          <div className='max-w-md text-center'>
            <h2 className='text-foreground text-lg font-semibold tracking-tight sm:text-xl'>
              {hasPending
                ? `你有 ${props.availableBoxes} 次待抽取`
                : '抽取一个盲盒'}
            </h2>
            <p className='text-muted-foreground mt-1.5 text-xs leading-5'>
              {hasPending
                ? '每开一个盲盒都会获得一个当前开奖周期有效的幸运号；20:00 后开出将参与次日开奖。'
                : '每次抽取都会获得一项奖励，并附赠一个当前开奖周期有效的四位幸运号。'}
            </p>
          </div>

          {hasPending ? (
            <div className='flex w-full max-w-sm flex-col gap-3'>
              <div
                className='bg-muted grid grid-cols-2 rounded-lg p-1'
                role='group'
                aria-label='盲盒开启方式'
              >
                <OpenModeButton
                  active={openMode === 'single'}
                  onClick={() => setOpenMode('single')}
                  icon={PackageOpen}
                >
                  逐个开启
                </OpenModeButton>
                <OpenModeButton
                  active={openMode === 'all'}
                  onClick={() => setOpenMode('all')}
                  icon={Boxes}
                >
                  全部打开
                </OpenModeButton>
              </div>
              <Button
                size='lg'
                onClick={() => props.onManualOpen(openCount)}
                disabled={opening}
                className='w-full whitespace-normal'
              >
                {opening ? (
                  <>
                    <Loader2
                      data-icon='inline-start'
                      className='animate-spin'
                    />
                    抽取中
                  </>
                ) : (
                  <>
                    <PackageOpen data-icon='inline-start' />
                    {openMode === 'single'
                      ? '开启 1 个盲盒'
                      : `全部打开 ${props.availableBoxes} 个`}
                  </>
                )}
              </Button>
            </div>
          ) : null}
        </div>
      </div>

      <div className='border-border/70 space-y-4 border-t px-4 py-4 sm:px-6 sm:py-5'>
        <div>
          <div className='flex flex-wrap items-baseline justify-between gap-2'>
            <span className='text-foreground text-sm font-semibold'>
              抽取次数
            </span>
            <span className='text-muted-foreground text-xs tabular-nums'>
              单价 ¥{props.unitPrice.toFixed(1)}
            </span>
          </div>
          <div className='mt-2.5 flex min-w-0 flex-wrap items-center gap-2'>
            {(props.countOptions.length > 0
              ? props.countOptions
              : [1, 3, 5, 10]
            ).map((value) => (
              <QuantityChip
                key={value}
                value={value}
                current={props.quantity}
                disabled={disabled}
                onSelect={props.onQuantityChange}
              />
            ))}
            <Input
              type='number'
              min={1}
              value={props.quantity}
              onChange={(event) => {
                const value = Number(event.target.value)
                props.onQuantityChange(
                  Number.isFinite(value) && value > 0 ? value : 1
                )
              }}
              className='h-9 w-20 max-w-full'
              aria-label='自定义抽取次数'
              disabled={disabled}
            />
          </div>
        </div>

        <div>
          <span className='text-foreground text-sm font-semibold'>
            支付方式
          </span>
          <div className='mt-2.5 flex min-w-0 flex-wrap gap-2'>
            {props.payMethods.map((method) => (
              <Button
                key={method.type}
                type='button'
                size='sm'
                variant={
                  props.paymentMethod?.type === method.type
                    ? 'default'
                    : 'outline'
                }
                onClick={() => props.onPaymentMethodChange(method)}
                disabled={disabled}
              >
                {getBlindBoxMethodLabel(method)}
              </Button>
            ))}
          </div>
        </div>

        <div className='border-primary/25 bg-primary/[0.045] flex min-w-0 flex-col items-stretch gap-3 rounded-xl border px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between'>
          <div>
            <div className='text-muted-foreground text-[11px]'>应付金额</div>
            <motion.div
              key={props.amountDue}
              className='text-foreground font-mono text-2xl font-semibold tabular-nums'
              initial={reduced ? false : { opacity: 0, y: -6 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.28, ease: EASE_OUT_QUINT }}
            >
              ¥{props.amountDue.toFixed(2)}
            </motion.div>
          </div>
          <div className='flex min-w-0 flex-col gap-2 sm:flex-row'>
            <Button
              type='button'
              variant='outline'
              className='w-full sm:w-auto'
              onClick={props.onOpenProps}
            >
              <Gift data-icon='inline-start' />
              我的道具{props.propCount > 0 ? ` (${props.propCount})` : ''}
            </Button>
            <Button
              onClick={props.onPay}
              disabled={!props.enabled || props.paying || !props.paymentMethod}
              className='w-full sm:w-auto sm:min-w-36'
            >
              {props.paying ? (
                <>
                  <Loader2 data-icon='inline-start' className='animate-spin' />
                  处理中
                </>
              ) : (
                <>
                  <Sparkles data-icon='inline-start' />
                  立即购买
                </>
              )}
            </Button>
          </div>
        </div>
      </div>
    </section>
  )
}

function OpenModeButton(props: {
  active: boolean
  onClick: () => void
  icon: typeof PackageOpen
  children: ReactNode
}) {
  const Icon = props.icon
  return (
    <button
      type='button'
      aria-pressed={props.active}
      onClick={props.onClick}
      className={cn(
        'focus-visible:ring-ring flex min-h-9 items-center justify-center gap-2 rounded-md px-3 text-sm font-medium transition-colors outline-none focus-visible:ring-2',
        props.active
          ? 'bg-background text-foreground shadow-sm'
          : 'text-muted-foreground hover:text-foreground'
      )}
    >
      <Icon className='size-4' aria-hidden='true' />
      {props.children}
    </button>
  )
}

function AmbientGlow(props: { reduced: boolean }) {
  return (
    <motion.div
      aria-hidden='true'
      className='bg-primary/12 pointer-events-none absolute top-2 left-1/2 size-52 -translate-x-1/2 rounded-full blur-3xl'
      animate={
        props.reduced
          ? undefined
          : { opacity: [0.5, 0.85, 0.5], scale: [1, 1.08, 1] }
      }
      transition={{ duration: 4.5, repeat: Infinity, ease: 'easeInOut' }}
    />
  )
}

function BoxFigure(props: {
  reduced: boolean
  pending: boolean
  opening: boolean
  onOpen?: () => void
}) {
  const interactive = Boolean(props.onOpen)
  const idle = props.reduced
    ? undefined
    : props.opening
      ? { rotate: [0, -5, 5, -4, 4, 0], y: 0 }
      : { y: [0, -7, 0] }

  return (
    <motion.button
      type='button'
      onClick={props.onOpen}
      disabled={!interactive}
      aria-label={interactive ? '立即抽取待开盲盒' : '盲盒'}
      className={cn(
        'relative block rounded-2xl outline-none',
        interactive
          ? 'focus-visible:ring-ring cursor-pointer focus-visible:ring-2 focus-visible:ring-offset-2'
          : 'cursor-default'
      )}
      animate={idle}
      transition={
        props.opening
          ? { duration: 0.55, repeat: Infinity, ease: 'easeInOut' }
          : { duration: 3.6, repeat: Infinity, ease: 'easeInOut' }
      }
      whileHover={interactive && !props.reduced ? { scale: 1.05 } : undefined}
      whileTap={interactive && !props.reduced ? { scale: 0.96 } : undefined}
    >
      <BoxArt pending={props.pending} reduced={props.reduced} />
    </motion.button>
  )
}

function BoxArt(props: { pending: boolean; reduced: boolean }) {
  return (
    <div className='relative size-32 sm:size-36'>
      <div
        className={cn(
          'absolute inset-x-1 bottom-0 h-[62%] rounded-xl border-2 shadow-lg',
          props.pending
            ? 'border-primary/50 bg-primary/12'
            : 'border-border bg-card'
        )}
      >
        <div
          className={cn(
            'absolute inset-y-0 left-1/2 w-3.5 -translate-x-1/2',
            props.pending ? 'bg-primary/35' : 'bg-muted-foreground/20'
          )}
        />
      </div>

      <motion.div
        className={cn(
          'absolute inset-x-0 top-[22%] h-[24%] rounded-lg border-2 shadow-md',
          props.pending
            ? 'border-primary/55 bg-primary/18'
            : 'border-border bg-muted'
        )}
        animate={
          props.reduced || !props.pending
            ? undefined
            : { rotate: [-2.5, 2.5, -2.5], y: [0, -3, 0] }
        }
        transition={{ duration: 2.4, repeat: Infinity, ease: 'easeInOut' }}
      >
        <div
          className={cn(
            'absolute inset-y-0 left-1/2 w-3.5 -translate-x-1/2',
            props.pending ? 'bg-primary/40' : 'bg-muted-foreground/20'
          )}
        />
      </motion.div>

      <motion.div
        className='absolute inset-x-0 top-[10%] flex justify-center'
        animate={props.reduced ? undefined : { scale: [1, 1.12, 1] }}
        transition={{ duration: 2.8, repeat: Infinity, ease: 'easeInOut' }}
      >
        <span
          className={cn(
            'flex size-9 items-center justify-center rounded-full border-2',
            props.pending
              ? 'border-primary/55 bg-primary text-primary-foreground'
              : 'border-border bg-card text-primary'
          )}
        >
          <Sparkles className='size-4' aria-hidden='true' />
        </span>
      </motion.div>
    </div>
  )
}

function QuantityChip(props: {
  value: number
  current: number
  disabled: boolean
  onSelect: (value: number) => void
}) {
  const active = props.value === props.current

  return (
    <motion.button
      type='button'
      onClick={() => props.onSelect(props.value)}
      disabled={props.disabled}
      whileTap={{ scale: 0.94 }}
      className={cn(
        'rounded-full border px-3.5 py-1.5 text-sm font-medium transition-colors disabled:opacity-50',
        active
          ? 'border-primary bg-primary text-primary-foreground'
          : 'border-border bg-background/80 text-foreground hover:border-foreground'
      )}
    >
      x{props.value}
    </motion.button>
  )
}
