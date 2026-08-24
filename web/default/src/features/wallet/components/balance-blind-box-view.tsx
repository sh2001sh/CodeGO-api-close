import {
  FlaskConical,
  Gift,
  Loader2,
  PackageOpen,
  Search,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react'
import { useReducedMotion } from 'motion/react'
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
import { BlindBoxPityTrack } from './blind-box-rules-panel'
import { BoxFigure } from './blind-box-stage'

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
      />
      <div className='space-y-5 px-4 py-4 sm:px-6 sm:py-5'>
        <BalanceBoxPurchaseWorkspace {...props} />
        <BalanceBoxSecondaryActions {...props} />
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
}) {
  const reduced = Boolean(useReducedMotion())
  const inventoryCount = props.balance?.inventory_count || 0
  return (
    <div className='border-primary/20 bg-primary/[0.045] border-b px-4 py-5 sm:px-6'>
      <div className='relative grid grid-cols-[96px_minmax(0,1fr)] items-center gap-3 sm:grid-cols-[120px_minmax(0,1fr)] sm:gap-5 lg:grid-cols-[150px_minmax(0,1fr)_minmax(220px,280px)]'>
        <div className='flex h-28 items-center justify-center sm:h-auto'>
          <div className='scale-75 sm:scale-100'>
            <BoxFigure
              reduced={reduced}
              pending={inventoryCount > 0}
              opening={props.opening}
              tone='primary'
              onOpen={
                inventoryCount > 0 && !props.busy
                  ? () => props.onOpenCount(1)
                  : undefined
              }
            />
          </div>
        </div>
        <div className='min-w-0 text-left'>
          <div className='text-primary text-xs font-semibold'>
            统一盲盒 · {(props.balance?.price_usd || 2.5).toFixed(2)} / 个
          </div>
          <h2 className='text-foreground mt-1 text-lg font-semibold tracking-tight sm:text-xl'>
            {inventoryCount > 0
              ? `你有 ${inventoryCount} 个统一盲盒待开启`
              : '购买可持有、可转赠的统一盲盒'}
          </h2>
          <p className='text-muted-foreground mt-1.5 text-xs leading-5'>
            人民币与统一额度购买进入同一库存、同一奖池。奖励在开启时抽取，购买和转赠不会提前推进保底。
          </p>
          <p className='text-primary/90 mt-1 text-[11px] leading-5'>
            普通池采用高波动结构；抽中“再来一抽”会立即补发 1 个待开启盲盒。
          </p>
          <BalanceBoxHeaderMetrics balance={props.balance} />
        </div>
        <BalanceBoxOpenActions
          inventoryCount={inventoryCount}
          busy={props.busy}
          opening={props.opening}
          onOpenCount={props.onOpenCount}
        />
      </div>
    </div>
  )
}

function BalanceBoxHeaderMetrics(props: { balance?: BalanceBlindBoxOverview }) {
  return (
    <div className='mt-4 grid w-full max-w-lg grid-cols-3 gap-3 border-t border-teal-500/15 pt-3 text-center lg:text-left'>
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
}) {
  if (props.inventoryCount <= 0) {
    return (
      <div className='border-border/70 bg-background/55 col-span-2 rounded-lg border px-4 py-3 text-center text-sm lg:col-span-1 lg:text-left'>
        <div className='font-medium'>库存为空</div>
        <div className='text-muted-foreground mt-1 text-xs leading-5'>
          在下方购买后，无需切换即可直接开启。
        </div>
      </div>
    )
  }
  const openAllCount = Math.min(100, props.inventoryCount)
  return (
    <div className='border-primary/20 bg-background/70 col-span-2 space-y-2 rounded-lg border p-3 lg:col-span-1'>
      <div className='text-sm font-semibold'>直接开启库存</div>
      <Button
        className='w-full'
        disabled={props.busy}
        onClick={() => props.onOpenCount(1)}
      >
        {props.opening ? (
          <Loader2 className='size-4 animate-spin' />
        ) : (
          <PackageOpen className='size-4' />
        )}
        {props.opening ? '开启中…' : '开启 1 个'}
      </Button>
      {openAllCount > 1 ? (
        <Button
          variant='outline'
          className='w-full'
          disabled={props.busy}
          onClick={() => props.onOpenCount(openAllCount)}
        >
          全部开启 {openAllCount} 个
        </Button>
      ) : null}
    </div>
  )
}

function BalanceBoxSecondaryActions(props: BalanceBoxPanelViewProps) {
  const expanded = ['simulation', 'gift', 'pity'].includes(props.mode)
  return (
    <div className='border-border/70 border-t pt-4'>
      <div className='flex flex-wrap items-center gap-2'>
        <span className='text-muted-foreground mr-auto text-xs'>更多操作</span>
        <Button
          size='sm'
          variant={props.mode === 'simulation' ? 'secondary' : 'outline'}
          onClick={() =>
            props.onModeChange(
              props.mode === 'simulation' ? 'purchase' : 'simulation'
            )
          }
        >
          <FlaskConical className='size-4' />
          模拟抽盒
        </Button>
        <Button
          size='sm'
          variant={props.mode === 'pity' ? 'secondary' : 'outline'}
          onClick={() =>
            props.onModeChange(props.mode === 'pity' ? 'purchase' : 'pity')
          }
        >
          <ShieldCheck className='size-4' />
          保底进度
        </Button>
        <Button
          size='sm'
          variant={props.mode === 'gift' ? 'secondary' : 'outline'}
          onClick={() =>
            props.onModeChange(props.mode === 'gift' ? 'purchase' : 'gift')
          }
        >
          <Gift className='size-4' />
          赠送库存
        </Button>
      </div>
      {expanded ? (
        <div className='mt-4'>
          {props.mode === 'simulation' ? (
            <BalanceBlindBoxSimulator balance={props.balance} />
          ) : props.mode === 'pity' ? (
            <BlindBoxPityTrack balance={props.balance} />
          ) : (
            <div className='grid gap-4 lg:grid-cols-[minmax(0,1fr)_300px]'>
              <BalanceBoxRecipientFields {...props} />
              <BalanceBoxActionControls {...props} />
            </div>
          )}
        </div>
      ) : null}
    </div>
  )
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
