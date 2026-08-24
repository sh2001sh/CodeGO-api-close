import {
  ArrowRight,
  Check,
  Coins,
  CreditCard,
  Loader2,
  WalletCards,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { BalanceBoxQuantityControl } from './balance-blind-box-controls'
import type { BalanceBoxPanelViewProps } from './balance-blind-box-view'

export function BalanceBoxPurchaseWorkspace(props: BalanceBoxPanelViewProps) {
  const unitPrice = props.balance?.price_usd || 2.5
  const totalPrice = unitPrice * props.count
  const inventoryAfter = (props.balance?.inventory_count || 0) + props.count

  return (
    <section aria-labelledby='blind-box-purchase-title'>
      <PurchaseHeader unitPrice={unitPrice} inventoryAfter={inventoryAfter} />
      <PurchaseGrid props={props} totalPrice={totalPrice} />

      <Button
        type='button'
        variant='ghost'
        size='sm'
        className='text-muted-foreground mt-2 ml-auto flex'
        onClick={props.onOpenProps}
      >
        <WalletCards className='size-4' aria-hidden='true' />
        我的权益卡
        <ArrowRight className='size-4' aria-hidden='true' />
      </Button>
    </section>
  )
}

function PurchaseHeader(props: { unitPrice: number; inventoryAfter: number }) {
  return (
    <div className='flex flex-wrap items-end justify-between gap-3'>
      <div>
        <h3
          id='blind-box-purchase-title'
          className='text-foreground text-base font-semibold'
        >
          购买盲盒
        </h3>
        <p className='text-muted-foreground mt-1 text-xs leading-5'>
          购买后进入上方库存，每人每日合计最多购买 10 个。
        </p>
      </div>
      <div className='text-right text-xs tabular-nums'>
        <span className='text-muted-foreground'>单价 </span>
        <span className='text-foreground font-semibold'>
          {props.unitPrice.toFixed(2)}
        </span>
        <span className='text-muted-foreground ml-3'>购买后库存 </span>
        <span className='text-foreground font-semibold'>
          {props.inventoryAfter} 个
        </span>
      </div>
    </div>
  )
}

function PurchaseGrid(props: {
  props: BalanceBoxPanelViewProps
  totalPrice: number
}) {
  const view = props.props
  return (
    <div className='border-border/70 mt-4 grid gap-5 border-y py-4 lg:grid-cols-[minmax(220px,0.9fr)_minmax(220px,1fr)_minmax(260px,1.05fr)] lg:gap-6'>
      <div className='min-w-0'>
        <BalanceBoxQuantityControl
          count={view.count}
          max={view.maxCount}
          disabled={view.busy}
          onChange={view.onCountChange}
        />
      </div>
      <div className='border-border/70 min-w-0 border-t pt-4 lg:border-t-0 lg:border-l lg:pt-0 lg:pl-6'>
        <div className='text-foreground text-sm font-semibold'>人民币渠道</div>
        {view.cashMethods.length > 0 ? (
          <CashMethodPicker {...view} />
        ) : (
          <p className='text-muted-foreground mt-2 text-xs leading-5'>
            当前没有可用的人民币支付渠道，可使用统一额度购买。
          </p>
        )}
      </div>
      <div className='border-border/70 min-w-0 border-t pt-4 lg:border-t-0 lg:border-l lg:pt-0 lg:pl-6'>
        <div className='flex items-end justify-between gap-3'>
          <div>
            <div className='text-muted-foreground text-xs'>订单合计</div>
            <div className='text-foreground mt-1 text-2xl font-semibold tabular-nums'>
              {props.totalPrice.toFixed(2)}
            </div>
          </div>
          <div className='text-muted-foreground text-right text-[11px] leading-5'>
            共 {view.count} 个
            <br />
            今日剩余 {view.balance?.remaining_purchase_limit || 0} 个
          </div>
        </div>
        <div className='mt-3'>
          <PurchasePaymentActions {...view} totalPrice={props.totalPrice} />
        </div>
      </div>
    </div>
  )
}

function CashMethodPicker(props: BalanceBoxPanelViewProps) {
  return (
    <div className='mt-2 grid gap-2 sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2'>
      {props.cashMethods.map((method) => {
        const selected = props.selectedCashMethod?.type === method.type
        return (
          <button
            key={method.type}
            type='button'
            aria-pressed={selected}
            onClick={() => props.onCashMethodChange(method)}
            disabled={props.busy}
            className={cn(
              'focus-visible:ring-ring flex min-h-10 min-w-0 items-center justify-between gap-2 rounded-md border px-3 text-left text-sm transition-colors outline-none focus-visible:ring-2 disabled:opacity-50',
              selected
                ? 'border-primary/45 bg-primary/10 text-foreground'
                : 'border-border bg-background/75 text-muted-foreground hover:border-primary/35 hover:text-foreground'
            )}
          >
            <span className='min-w-0 truncate'>{method.name}</span>
            <span
              className={cn(
                'flex size-4 shrink-0 items-center justify-center rounded-full border',
                selected
                  ? 'border-primary bg-primary text-primary-foreground'
                  : 'border-border'
              )}
            >
              {selected ? <Check className='size-2.5' /> : null}
            </span>
          </button>
        )
      })}
    </div>
  )
}

function PurchasePaymentActions(
  props: BalanceBoxPanelViewProps & { totalPrice: number }
) {
  const walletBalance = props.balance?.balance_usd || 0
  const walletShortfall = Math.max(0, props.totalPrice - walletBalance)
  const cashLimitReached =
    (props.balance?.remaining_purchase_limit || 0) < props.count
  return (
    <div className='space-y-2'>
      <PurchaseButton
        busy={props.busy && !props.cashPaying}
        disabled={!props.canPurchase}
        icon={Coins}
        title='统一额度购买'
        detail={
          walletShortfall > 0
            ? `还差 ${walletShortfall.toFixed(2)} 统一额度`
            : `可用 ${walletBalance.toFixed(2)} 统一额度`
        }
        amount={props.totalPrice.toFixed(2)}
        busyLabel='正在购买…'
        onClick={props.onPurchase}
      />
      {props.cashMethods.length > 0 ? (
        <PurchaseButton
          busy={props.cashPaying}
          disabled={!props.selectedCashMethod || props.busy || cashLimitReached}
          icon={CreditCard}
          title='人民币购买'
          detail={props.selectedCashMethod?.name || '请先选择支付渠道'}
          amount={`¥${props.cashAmountDue.toFixed(2)}`}
          busyLabel='正在创建订单…'
          onClick={props.onCashPurchase}
        />
      ) : null}
    </div>
  )
}

function PurchaseButton(props: {
  busy: boolean
  disabled: boolean
  icon: typeof Coins
  title: string
  detail: string
  amount: string
  busyLabel: string
  onClick: () => void
}) {
  const Icon = props.icon
  return (
    <Button
      type='button'
      className='h-auto min-h-11 w-full min-w-0 justify-between gap-3 px-3 py-2 whitespace-normal'
      disabled={props.disabled}
      onClick={props.onClick}
    >
      <span className='flex min-w-0 items-center gap-2 text-left'>
        {props.busy ? (
          <Loader2 className='size-4 shrink-0 animate-spin' />
        ) : (
          <Icon className='size-4 shrink-0' />
        )}
        <span className='min-w-0'>
          <span className='block text-xs font-semibold'>
            {props.busy ? props.busyLabel : props.title}
          </span>
          <span className='block truncate text-[10px] font-normal opacity-75'>
            {props.detail}
          </span>
        </span>
      </span>
      <span className='max-w-[42%] shrink-0 text-right text-xs font-semibold break-all tabular-nums'>
        {props.amount}
      </span>
    </Button>
  )
}
