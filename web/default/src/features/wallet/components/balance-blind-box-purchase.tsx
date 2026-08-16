import {
  ArrowRight,
  Check,
  Coins,
  CreditCard,
  Layers3,
  Loader2,
  WalletCards,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { BalanceBoxQuantityControl } from './balance-blind-box-controls'
import type { BalanceBoxPanelViewProps } from './balance-blind-box-view'
import { BlindBoxPityTrack } from './blind-box-rules-panel'

export function BalanceBoxPurchaseWorkspace(props: BalanceBoxPanelViewProps) {
  const unitPrice = props.balance?.price_usd || 2.5
  const totalPrice = unitPrice * props.count

  return (
    <div className='grid min-w-0 gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(340px,0.72fr)] lg:gap-6'>
      <div className='min-w-0 space-y-5'>
        <PurchaseSummary {...props} totalPrice={totalPrice} />
        <BlindBoxPityTrack balance={props.balance} />
      </div>
      <PurchaseCheckout {...props} totalPrice={totalPrice} />
    </div>
  )
}

function PurchaseSummary(
  props: BalanceBoxPanelViewProps & { totalPrice: number }
) {
  const inventoryAfter = (props.balance?.inventory_count || 0) + props.count
  const remainingAfter = Math.max(
    0,
    (props.balance?.remaining_purchase_limit || 0) - props.count
  )

  return (
    <section className='border-border/70 border-b pb-5'>
      <div className='app-section-kicker'>本次购买</div>
      <div className='mt-2 flex flex-col justify-between gap-4 sm:flex-row sm:items-end'>
        <div>
          <h3 className='text-foreground text-xl font-semibold'>
            {props.count} 个统一盲盒
          </h3>
          <p className='text-muted-foreground mt-1 max-w-xl text-sm leading-6'>
            支付完成后进入库存，奖励立即封存；开启只负责揭晓结果。人民币与统一额度购买使用同一奖池和同一保底进度。
          </p>
        </div>
        <div className='text-left sm:text-right'>
          <div className='text-muted-foreground text-xs'>统一额度价值</div>
          <div className='text-foreground mt-1 text-2xl font-semibold tabular-nums'>
            {props.totalPrice.toFixed(2)}
          </div>
        </div>
      </div>

      <div className='mt-5 grid grid-cols-3 divide-x border-y py-3'>
        <PurchaseMetric
          label='购买数量'
          value={`${props.count} 个`}
          icon={Layers3}
        />
        <PurchaseMetric label='入库后' value={`${inventoryAfter} 个`} />
        <PurchaseMetric label='今日剩余' value={`${remainingAfter} 个`} />
      </div>
    </section>
  )
}

function PurchaseMetric(props: {
  label: string
  value: string
  icon?: typeof Layers3
}) {
  const Icon = props.icon
  return (
    <div className='min-w-0 px-3 first:pl-0 last:pr-0'>
      <div className='text-muted-foreground flex items-center gap-1.5 text-[11px]'>
        {Icon ? <Icon className='size-3' aria-hidden='true' /> : null}
        {props.label}
      </div>
      <div className='text-foreground mt-1 truncate text-sm font-semibold tabular-nums'>
        {props.value}
      </div>
    </div>
  )
}

function PurchaseCheckout(
  props: BalanceBoxPanelViewProps & { totalPrice: number }
) {
  return (
    <aside className='border-primary/20 bg-primary/[0.035] min-w-0 space-y-5 rounded-lg border p-4 sm:p-5'>
      <div>
        <div className='app-section-kicker'>结算</div>
        <h3 className='text-foreground mt-1 text-base font-semibold'>
          选择数量与支付方式
        </h3>
      </div>

      <BalanceBoxQuantityControl
        count={props.count}
        max={props.maxCount}
        disabled={props.busy}
        onChange={props.onCountChange}
      />

      <CheckoutSummary {...props} />

      {props.cashMethods.length > 0 ? <CashMethodPicker {...props} /> : null}

      <PurchasePaymentActions {...props} />

      <Button
        type='button'
        variant='ghost'
        className='text-muted-foreground w-full justify-between'
        onClick={props.onOpenProps}
      >
        <span className='flex items-center gap-2'>
          <WalletCards className='size-4' aria-hidden='true' />
          我的权益卡
        </span>
        <ArrowRight className='size-4' aria-hidden='true' />
      </Button>
    </aside>
  )
}

function CheckoutSummary(
  props: BalanceBoxPanelViewProps & { totalPrice: number }
) {
  return (
    <div className='border-border/70 space-y-2 border-y py-4 text-sm'>
      <CheckoutRow
        label='盲盒单价'
        value={`${(props.balance?.price_usd || 2.5).toFixed(2)}`}
      />
      <CheckoutRow label='购买数量' value={`x ${props.count}`} />
      <div className='flex items-end justify-between gap-3 pt-1'>
        <span className='text-foreground font-medium'>订单合计</span>
        <span className='text-foreground text-2xl font-semibold tabular-nums'>
          {props.totalPrice.toFixed(2)}
        </span>
      </div>
    </div>
  )
}

function PurchasePaymentActions(
  props: BalanceBoxPanelViewProps & { totalPrice: number }
) {
  const walletBalance = props.balance?.balance_usd || 0
  const walletShortfall = Math.max(0, props.totalPrice - walletBalance)
  return (
    <div className='space-y-2.5'>
      {props.cashMethods.length > 0 ? (
        <PurchaseButton
          busy={props.cashPaying}
          disabled={!props.selectedCashMethod || props.busy}
          icon={CreditCard}
          title='人民币支付'
          detail={props.selectedCashMethod?.name || '请先选择支付渠道'}
          amount={`¥${props.cashAmountDue.toFixed(2)}`}
          busyLabel='正在创建订单…'
          onClick={props.onCashPurchase}
        />
      ) : null}
      <PurchaseButton
        busy={props.busy && !props.cashPaying}
        disabled={!props.canPurchase}
        icon={Coins}
        title='统一额度支付'
        detail={
          walletShortfall > 0
            ? `还差 ${walletShortfall.toFixed(2)} 统一额度`
            : `可用 ${walletBalance.toFixed(2)} 统一额度`
        }
        amount={props.totalPrice.toFixed(2)}
        busyLabel='正在购买…'
        onClick={props.onPurchase}
      />
    </div>
  )
}

function CashMethodPicker(props: BalanceBoxPanelViewProps) {
  return (
    <div>
      <div className='text-foreground text-sm font-semibold'>
        人民币支付渠道
      </div>
      <div
        className='mt-2 grid gap-2 sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2'
        aria-label='人民币支付方式'
      >
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
    </div>
  )
}

function CheckoutRow(props: { label: string; value: string }) {
  return (
    <div className='flex items-center justify-between gap-3'>
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-foreground font-medium tabular-nums'>
        {props.value}
      </span>
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
      className='h-auto min-h-12 w-full min-w-0 justify-between gap-3 px-3 py-2.5 whitespace-normal'
      disabled={props.disabled}
      onClick={props.onClick}
    >
      <span className='flex min-w-0 items-center gap-2.5 text-left'>
        {props.busy ? (
          <Loader2 className='size-4 shrink-0 animate-spin' />
        ) : (
          <Icon className='size-4 shrink-0' />
        )}
        <span className='min-w-0'>
          <span className='block text-sm font-semibold'>
            {props.busy ? props.busyLabel : props.title}
          </span>
          <span className='block truncate text-[10px] font-normal opacity-75'>
            {props.detail}
          </span>
        </span>
      </span>
      <span className='max-w-[45%] shrink-0 text-right text-sm font-semibold break-all tabular-nums'>
        {props.amount}
      </span>
    </Button>
  )
}
