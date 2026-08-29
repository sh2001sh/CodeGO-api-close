import {
  FlaskConical,
  Gift,
  Loader2,
  PackageOpen,
  Search,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type {
  BalanceBlindBoxOverview,
  PaymentMethod,
  WalletTransferRecipient,
} from '../types'
import {
  BalanceBoxMetric,
  BalanceBoxQuantityControl,
} from './balance-blind-box-controls'
import { BalanceBoxGiftConfirm } from './balance-blind-box-gift-confirm'
import { BalanceBoxPurchaseWorkspace } from './balance-blind-box-purchase'
import { BalanceBlindBoxSimulator } from './balance-blind-box-simulator'
import { EditorialBoxGlyph } from './blind-box-editorial-glyph'
import { BlindBoxPityTrack } from './blind-box-rules-panel'

export type ActionMode = 'purchase' | 'open' | 'gift' | 'simulation' | 'pity'

export interface BalanceBoxPanelViewProps {
  balance?: BalanceBlindBoxOverview
  mode: ActionMode
  count: number
  maxCount: number
  inventoryActionCount: number
  inventoryMaxCount: number
  busy: boolean
  recipient: WalletTransferRecipient | null
  recipientId: string
  canPurchase: boolean
  canUseInventory: boolean
  confirmGift: boolean
  cashMethods: PaymentMethod[]
  selectedCashMethod: PaymentMethod | null
  cashAmountDue: number
  cashPaying: boolean
  onModeChange: (mode: ActionMode) => void
  onCountChange: (count: number) => void
  onInventoryCountChange: (count: number) => void
  onRecipientIdChange: (value: string) => void
  onPurchase: () => void
  onOpen: () => void
  onOpenCount: (count: number) => void
  onLookup: () => void
  onGift: () => void
  onConfirmGiftChange: (open: boolean) => void
  onCashMethodChange: (method: PaymentMethod) => void
  onCashPurchase: () => void
  onOpenProps: () => void
}

export function BalanceBoxPanelView(props: BalanceBoxPanelViewProps) {
  return (
    <section className='app-page-shell overflow-hidden'>
      <BalanceBoxHeader
        balance={props.balance}
        opening={props.busy && props.mode === 'open'}
        busy={props.busy}
        onOpenCount={props.onOpenCount}
        mode={props.mode}
        onModeChange={props.onModeChange}
      />
      <div className='px-4 py-4 sm:px-6 sm:py-5'>
        <BalanceBoxPurchaseWorkspace {...props} />
        <BalanceBoxSecondaryPanel {...props} />
      </div>
      <BalanceBoxGiftConfirm {...props} count={props.inventoryActionCount} />
    </section>
  )
}

function BalanceBoxHeader(props: {
  balance?: BalanceBlindBoxOverview
  opening: boolean
  busy: boolean
  onOpenCount: (count: number) => void
  mode: ActionMode
  onModeChange: (mode: ActionMode) => void
}) {
  const inventoryCount = props.balance?.inventory_count || 0
  return (
    <div className='relative overflow-hidden border-b px-4 py-5 sm:px-6 sm:py-6'>
      <span
        aria-hidden
        className='border-primary/[0.08] pointer-events-none absolute -top-28 -right-20 size-64 rounded-full border'
      />
      <div className='relative grid gap-7 sm:grid-cols-[auto_minmax(0,1fr)] lg:grid-cols-[auto_minmax(0,1fr)_minmax(220px,264px)] lg:items-end'>
        <EditorialBoxGlyph
          pending={inventoryCount > 0}
          opening={props.opening}
          busy={props.busy}
          onOpen={() => props.onOpenCount(1)}
        />
        <div className='min-w-0'>
          <div className='flex items-center gap-2.5'>
            <span aria-hidden className='bg-primary block h-2 w-2' />
            <span className='codego-kicker'>
              统一盲盒 · {(props.balance?.price_usd || 2.5).toFixed(2)} / 个
            </span>
          </div>
          <h2 className='text-foreground mt-3 text-2xl leading-[1.06] font-semibold text-balance sm:text-3xl'>
            {inventoryCount > 0 ? `${inventoryCount} 个盲盒待开启` : '统一盲盒'}
          </h2>
          <BalanceBoxHeaderMetrics balance={props.balance} />
        </div>
        <BalanceBoxOpenActions
          inventoryCount={inventoryCount}
          busy={props.busy}
          opening={props.opening}
          onOpenCount={props.onOpenCount}
          mode={props.mode}
          onModeChange={props.onModeChange}
        />
      </div>
    </div>
  )
}

function BalanceBoxHeaderMetrics(props: { balance?: BalanceBlindBoxOverview }) {
  return (
    <div className='codego-fact-row mt-6 grid max-w-md grid-cols-3'>
      <BalanceBoxMetric
        label='库存'
        value={`${props.balance?.inventory_count || 0} 个`}
      />
      <BalanceBoxMetric
        label='今日购买'
        value={`${props.balance?.purchased_today || 0}/${props.balance?.daily_purchase_limit || 10}`}
      />
      <BalanceBoxMetric
        label='统一额度'
        value={`${(props.balance?.balance_usd || 0).toFixed(2)}`}
      />
    </div>
  )
}

function BalanceBoxOpenActions(props: {
  inventoryCount: number
  busy: boolean
  opening: boolean
  onOpenCount: (count: number) => void
  mode: ActionMode
  onModeChange: (mode: ActionMode) => void
}) {
  const ops = [
    { mode: 'simulation' as ActionMode, icon: FlaskConical, label: '模拟抽盒' },
    { mode: 'pity' as ActionMode, icon: ShieldCheck, label: '保底进度' },
    { mode: 'gift' as ActionMode, icon: Gift, label: '赠送库存' },
  ]

  if (props.inventoryCount <= 0) {
    return (
      <div className='space-y-3'>
        <div className='codego-stat-label'>库存操作</div>
        <div className='grid grid-cols-3 gap-1.5'>
          {ops.map((op) => (
            <InventoryOpButton
              key={op.mode}
              {...op}
              active={props.mode === op.mode}
              disabled={props.busy}
              onClick={() =>
                props.onModeChange(
                  props.mode === op.mode ? 'purchase' : op.mode
                )
              }
            />
          ))}
        </div>
      </div>
    )
  }
  const openAllCount = Math.min(100, props.inventoryCount)
  return (
    <div className='space-y-3'>
      <div className='codego-stat-label'>直接开启</div>
      <div className='grid grid-cols-2 gap-2'>
        <Button
          className='h-11 flex-1 px-2.5'
          disabled={props.busy}
          onClick={() => props.onOpenCount(1)}
        >
          {props.opening ? (
            <Loader2 className='size-4 animate-spin' />
          ) : (
            <PackageOpen className='size-4' />
          )}
          开启 1 个
        </Button>
        <Button
          variant='outline'
          className='h-11 flex-1 px-2.5'
          disabled={props.busy}
          onClick={() => props.onOpenCount(openAllCount)}
        >
          全部 {openAllCount} 个
        </Button>
      </div>
      <div className='codego-stat-label border-border/60 border-t pt-3'>
        库存操作
      </div>
      <div className='grid grid-cols-3 gap-1.5'>
        {ops.map((op) => (
          <InventoryOpButton
            key={op.mode}
            {...op}
            active={props.mode === op.mode}
            disabled={props.busy}
            onClick={() =>
              props.onModeChange(props.mode === op.mode ? 'purchase' : op.mode)
            }
          />
        ))}
      </div>
    </div>
  )
}

function InventoryOpButton(props: {
  mode: ActionMode
  icon: LucideIcon
  label: string
  active: boolean
  disabled: boolean
  onClick: () => void
}) {
  const Icon = props.icon
  return (
    <button
      type='button'
      onClick={props.onClick}
      disabled={props.disabled}
      aria-pressed={props.active}
      className={cn(
        'flex min-w-0 items-center justify-center gap-1.5 border px-1.5 py-2 text-xs transition-colors disabled:opacity-50',
        props.active
          ? 'border-primary bg-primary text-primary-foreground font-semibold'
          : 'border-border bg-background/70 text-muted-foreground hover:border-primary/45 hover:text-foreground'
      )}
    >
      <Icon className='size-3.5 shrink-0' />
      <span className='truncate'>{props.label}</span>
    </button>
  )
}

function BalanceBoxSecondaryPanel(props: BalanceBoxPanelViewProps) {
  if (props.mode === 'simulation') {
    return (
      <div className='border-border/70 mt-4 border-t pt-4'>
        <BalanceBlindBoxSimulator balance={props.balance} />
      </div>
    )
  }
  if (props.mode === 'pity') {
    return (
      <div className='border-border/70 mt-4 border-t pt-4'>
        <BlindBoxPityTrack balance={props.balance} />
      </div>
    )
  }
  if (props.mode === 'gift') {
    return (
      <div className='border-border/70 mt-4 border-t pt-4'>
        <div className='grid gap-4 lg:grid-cols-[minmax(0,1fr)_300px]'>
          <BalanceBoxRecipientFields {...props} />
          <BalanceBoxActionControls {...props} />
        </div>
      </div>
    )
  }
  return null
}

function BalanceBoxRecipientFields(props: BalanceBoxPanelViewProps) {
  return (
    <div className='space-y-2'>
      <label className='text-sm font-medium' htmlFor='balance-box-recipient'>
        接收方公开 ID
      </label>
      <div className='flex gap-2'>
        <Input
          id='balance-box-recipient'
          maxLength={6}
          value={props.recipientId}
          onChange={(event) =>
            props.onRecipientIdChange(event.target.value.toUpperCase())
          }
          placeholder='例如 A1B2C3'
          className='font-mono uppercase'
        />
        <Button
          variant='outline'
          size='icon'
          onClick={props.onLookup}
          disabled={props.busy}
          title='查找用户'
        >
          <Search className='size-4' />
        </Button>
      </div>
      {props.recipient ? (
        <div className='border-border bg-muted/40 rounded-md border px-3 py-2 text-sm'>
          接收方：{props.recipient.display_name_masked} ·{' '}
          {props.recipient.external_id}
        </div>
      ) : null}
      <p className='text-muted-foreground text-xs leading-5'>
        赠送不可撤销，盲盒可被接收方继续转赠。封存奖励不会因所有权变化而重新抽取。
      </p>
    </div>
  )
}

function BalanceBoxActionControls(props: BalanceBoxPanelViewProps) {
  return (
    <div className='border-primary/20 bg-primary/[0.035] space-y-4 rounded-lg border p-4'>
      <BalanceBoxQuantityControl
        count={props.inventoryActionCount}
        max={props.inventoryMaxCount}
        disabled={props.busy}
        onChange={props.onInventoryCountChange}
      />
      <BalanceBoxInventoryAction {...props} />
    </div>
  )
}

function BalanceBoxInventoryAction(props: BalanceBoxPanelViewProps) {
  const gifting = props.mode === 'gift'
  return (
    <BalanceBoxPrimaryButton
      busy={props.busy}
      disabled={!props.canUseInventory || (gifting && !props.recipient)}
      icon={gifting ? Gift : PackageOpen}
      busyLabel={gifting ? '赠送中…' : '开启中…'}
      label={`${gifting ? '赠送' : '开启'} ${props.inventoryActionCount} 个`}
      onClick={gifting ? () => props.onConfirmGiftChange(true) : props.onOpen}
    />
  )
}

function BalanceBoxPrimaryButton(props: {
  busy: boolean
  disabled: boolean
  icon: LucideIcon
  busyLabel: string
  label: string
  onClick: () => void
}) {
  const Icon = props.icon
  return (
    <Button
      className='w-full'
      disabled={props.disabled}
      onClick={props.onClick}
    >
      {props.busy ? (
        <Loader2 className='size-4 animate-spin' />
      ) : (
        <Icon className='size-4' />
      )}
      {props.busy ? props.busyLabel : props.label}
    </Button>
  )
}
