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
      <div className='flex items-center gap-2.5'>
        <span aria-hidden className='bg-primary block h-3 w-[3px]' />
        <h3
          id='blind-box-purchase-title'
          className='text-foreground text-[13px] font-semibold'
        >
          购买盲盒
        </h3>
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
    <>
      <div className='border-border/70 mt-4 grid gap-5 border-y py-4 lg:grid-cols-[minmax(200px,0.8fr)_minmax(220px,1fr)_minmax(220px,0.9fr)] lg:gap-6'>
        <div className='min-w-0'>
          <BalanceBoxQuantityControl
            count={view.count}
            max={view.maxCount}
            disabled={view.busy}
            onChange={view.onCountChange}
          />
        </div>
        <div className='border-border/70 min-w-0 border-t pt-4 lg:border-t-0 lg:border-l lg:pt-0 lg:pl-6'>
          <div className='codego-stat-label'>人民币渠道</div>
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
              <div className='codego-stat-label'>订单合计</div>
              <div className='text-foreground mt-2 text-2xl leading-none font-semibold tabular-nums'>
                {props.totalPrice.toFixed(2)}
              </div>
            </div>
            <div className='text-muted-foreground text-right text-[11px] leading-5'>
              共 {view.count} 个
              <br />
              今日剩余 {view.balance?.remaining_purchase_limit || 0} 个
            </div>
          </div>
        </div>
      </div>

      <div className='border-border/60 mt-3 grid gap-2 border-t pt-3 sm:grid-cols-2'>
        <PurchasePaymentActions {...view} totalPrice={props.totalPrice} />
      </div>
    </>
  )
}

function CashMethodPicker(props: BalanceBoxPanelViewProps) {
  return (
    <div className='mt-1'>
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
              'border-border/60 flex w-full min-w-0 items-center justify-between gap-3 border-b py-2.5 text-left text-[13px] transition-colors first:border-t-0 disabled:opacity-50',
              selected
                ? 'text-foreground'
                : 'text-muted-foreground hover:text-foreground'
            )}
          >
            <span className='min-w-0 truncate'>{method.name}</span>
            <span
              className={cn(
                'flex size-3.5 shrink-0 items-center justify-center border',
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
    <>
      <PurchaseButton
        busy={props.busy && !props.cashPaying}
        disabled={!props.canPurchase}
        icon={Coins}
        title='额度支付'
        detail={
          walletShortfall > 0
            ? `差 ${walletShortfall.toFixed(2)}`
            : `余额 ${walletBalance.toFixed(2)}`
        }
        amount={props.totalPrice.toFixed(2)}
        busyLabel='支付中…'
        onClick={props.onPurchase}
      />
      {props.cashMethods.length > 0 ? (
        <PurchaseButton
          variant='outline'
          busy={props.cashPaying}
          disabled={!props.selectedCashMethod || props.busy || cashLimitReached}
          icon={CreditCard}
          title='人民币支付'
          detail={props.selectedCashMethod?.name || '未选渠道'}
          amount={`¥${props.cashAmountDue.toFixed(2)}`}
          busyLabel='下单中…'
          onClick={props.onCashPurchase}
        />
      ) : null}
    </>
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
  variant?: 'default' | 'outline'
  onClick: () => void
}) {
  const Icon = props.icon
  return (
    <Button
      type='button'
      variant={props.variant}
      className='h-10 w-full min-w-0 justify-between gap-2 px-3'
      disabled={props.disabled}
      onClick={props.onClick}
    >
      <span className='flex min-w-0 items-center gap-2 text-left'>
        {props.busy ? (
          <Loader2 className='size-4 shrink-0 animate-spin' />
        ) : (
          <Icon className='size-4 shrink-0' />
        )}
        <span className='truncate text-xs font-semibold'>
          {props.busy ? props.busyLabel : props.title}
        </span>
        <span className='hidden truncate text-[10px] font-normal opacity-75 sm:inline'>
          {props.detail}
        </span>
      </span>
      <span className='shrink-0 text-right text-xs font-semibold tabular-nums'>
        {props.amount}
      </span>
    </Button>
  )
}
