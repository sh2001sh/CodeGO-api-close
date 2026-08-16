import {
  Coins,
  CreditCard,
  Gift,
  Loader2,
  PackageOpen,
  Search,
  ShoppingBag,
  FlaskConical,
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
  BalanceBoxModeButton,
  BalanceBoxQuantityControl,
} from './balance-blind-box-controls'
import { BalanceBoxGiftConfirm } from './balance-blind-box-gift-confirm'
import { BoxFigure } from './blind-box-stage'
import { BalanceBlindBoxSimulator } from './balance-blind-box-simulator'

export type ActionMode = 'purchase' | 'open' | 'gift' | 'simulation'

export interface BalanceBoxPanelViewProps {
  balance?: BalanceBlindBoxOverview
  mode: ActionMode
  count: number
  maxCount: number
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
  onRecipientIdChange: (value: string) => void
  onPurchase: () => void
  onOpen: () => void
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
      />
      <div className='space-y-4 px-4 py-4 sm:px-6 sm:py-5'>
        <BalanceBoxModeSwitch mode={props.mode} onChange={props.onModeChange} />
        <BalanceBoxWorkspace {...props} />
      </div>
      <BalanceBoxGiftConfirm {...props} />
    </section>
  )
}

function BalanceBoxHeader(props: {
  balance?: BalanceBlindBoxOverview
  opening: boolean
}) {
  const reduced = Boolean(useReducedMotion())
  const inventoryCount = props.balance?.inventory_count || 0
  return (
    <div className='border-primary/20 bg-primary/[0.045] border-b px-4 pt-7 pb-5 sm:px-6'>
      <div className='relative flex flex-col items-center gap-4'>
        <BoxFigure
          reduced={reduced}
          pending={inventoryCount > 0}
          opening={props.opening}
          tone='primary'
        />
        <div className='max-w-xl text-center'>
          <div className='text-primary text-xs font-semibold'>
            统一盲盒 · {(props.balance?.price_usd || 2.5).toFixed(2)} / 个
          </div>
          <h2 className='text-foreground mt-1 text-lg font-semibold tracking-tight sm:text-xl'>
            {inventoryCount > 0
              ? `你有 ${inventoryCount} 个统一盲盒待开启`
              : '购买可持有、可转赠的统一盲盒'}
          </h2>
          <p className='text-muted-foreground mt-1.5 text-xs leading-5'>
            人民币与统一额度购买进入同一库存、同一奖池。奖励在入库时封存，转赠不会改变结果。
          </p>
          <p className='text-primary/90 mt-1 text-[11px] leading-5'>
            奖励为公开概率的随机额度区间或权益卡；统一额度奖励永久有效。
          </p>
        </div>
        <BalanceBoxHeaderMetrics balance={props.balance} />
      </div>
    </div>
  )
}

function BalanceBoxHeaderMetrics(props: { balance?: BalanceBlindBoxOverview }) {
  return (
    <div className='grid w-full max-w-lg grid-cols-3 gap-3 border-t border-teal-500/15 pt-4 text-center'>
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

function BalanceBoxModeSwitch(props: {
  mode: ActionMode
  onChange: (mode: ActionMode) => void
}) {
  return (
    <div className='bg-muted grid grid-cols-2 rounded-lg p-1 sm:grid-cols-4' role='tablist'>
      <BalanceBoxModeButton
        active={props.mode === 'purchase'}
        icon={ShoppingBag}
        label='购买盲盒'
        onClick={() => props.onChange('purchase')}
      />
      <BalanceBoxModeButton
        active={props.mode === 'simulation'}
        icon={FlaskConical}
        label='模拟抽盒'
        onClick={() => props.onChange('simulation')}
      />
      <BalanceBoxModeButton
        active={props.mode === 'open'}
        icon={PackageOpen}
        label='开启库存'
        onClick={() => props.onChange('open')}
      />
      <BalanceBoxModeButton
        active={props.mode === 'gift'}
        icon={Gift}
        label='赠送库存'
        onClick={() => props.onChange('gift')}
      />
    </div>
  )
}

function BalanceBoxWorkspace(props: BalanceBoxPanelViewProps) {
  if (props.mode === 'simulation') {
    return <BalanceBlindBoxSimulator priceUSD={props.balance?.price_usd || 2.5} />
  }
  return (
    <div className='grid gap-4 lg:grid-cols-[minmax(0,1fr)_300px]'>
      <div className='space-y-3'>
        <BalanceBoxModeDescription mode={props.mode} balance={props.balance} />
        {props.mode === 'gift' ? (
          <BalanceBoxRecipientFields {...props} />
        ) : null}
      </div>
      <BalanceBoxActionControls {...props} />
    </div>
  )
}

function BalanceBoxModeDescription(props: {
  mode: ActionMode
  balance?: BalanceBlindBoxOverview
}) {
  if (props.mode === 'purchase') {
    return (
      <p className='text-muted-foreground text-sm leading-6'>
        今日还可购买 {props.balance?.remaining_purchase_limit || 0}{' '}
        个。付款成功后只增加库存，不会直接开奖；每日限制只计算本人购买，不限制收赠和开启。
      </p>
    )
  }
  if (props.mode === 'open') {
    return (
      <p className='text-muted-foreground text-sm leading-6'>
        从最早入库的盲盒开始开启，单次最多 100
        个。奖励直接发放到当前持有者账户，开启后不可转赠。
      </p>
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
        count={props.count}
        max={props.maxCount}
        disabled={props.busy}
        onChange={props.onCountChange}
      />
      {props.mode === 'purchase' ? (
        <BalanceBoxPurchaseAction {...props} />
      ) : null}
      {props.mode !== 'purchase' ? (
        <BalanceBoxInventoryAction {...props} />
      ) : null}
    </div>
  )
}

function BalanceBoxPurchaseAction(props: BalanceBoxPanelViewProps) {
  const totalPrice = (props.balance?.price_usd || 2.5) * props.count
  return (
    <div className='border-primary/15 space-y-3 border-t pt-3'>
      <div className='flex items-end justify-between gap-3'>
        <div>
          <div className='text-muted-foreground text-[11px]'>应付金额</div>
          <div className='text-foreground text-2xl font-semibold tabular-nums'>
            {totalPrice.toFixed(2)}
          </div>
        </div>
        <span className='text-muted-foreground max-w-28 text-right text-[11px] leading-4'>
          两种支付方式，奖池一致
        </span>
      </div>
      {props.cashMethods.length > 0 ? (
        <div className='flex flex-wrap gap-2' aria-label='人民币支付方式'>
          {props.cashMethods.map((method) => (
            <Button
              key={method.type}
              type='button'
              size='sm'
              variant={
                props.selectedCashMethod?.type === method.type
                  ? 'default'
                  : 'outline'
              }
              onClick={() => props.onCashMethodChange(method)}
              disabled={props.busy}
            >
              {method.name}
            </Button>
          ))}
        </div>
      ) : null}
      <div className='grid gap-2 sm:grid-cols-2'>
        <BalanceBoxPrimaryButton
          busy={props.cashPaying}
          disabled={!props.selectedCashMethod || props.busy}
          icon={CreditCard}
          busyLabel='创建订单…'
          label={`人民币支付 ${props.cashAmountDue.toFixed(2)}`}
          onClick={props.onCashPurchase}
        />
        <BalanceBoxPrimaryButton
          busy={props.busy}
          disabled={!props.canPurchase}
          icon={Coins}
          busyLabel='购买中…'
          label={`统一额度支付 ${totalPrice.toFixed(2)}`}
          onClick={props.onPurchase}
        />
      </div>
      <Button
        type='button'
        variant='ghost'
        className='w-full'
        onClick={props.onOpenProps}
      >
        查看我的权益卡
      </Button>
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
      label={`${gifting ? '赠送' : '开启'} ${props.count} 个`}
      onClick={gifting ? () => props.onConfirmGiftChange(true) : props.onOpen}
    />
  )
}

function BalanceBoxPrimaryButton(props: {
  busy: boolean
  disabled: boolean
  icon: typeof ShoppingBag
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
