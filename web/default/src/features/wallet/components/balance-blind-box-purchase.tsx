import { Check, Coins, CreditCard, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { BalanceBoxQuantityControl } from './balance-blind-box-controls'
import type { BalanceBoxPanelViewProps } from './balance-blind-box-view'

export function BalanceBoxPurchaseWorkspace(props: BalanceBoxPanelViewProps) {
  const { t } = useTranslation()
  const totalPrice = props.balance
    ? props.balance.price_usd * props.count
    : null
  const walletBalance = props.balance?.balance_usd ?? 0
  const walletShortfall = Math.max(0, (totalPrice ?? 0) - walletBalance)
  const cashLimitReached =
    (props.balance?.remaining_purchase_limit ?? 0) < props.count

  return (
    <section
      className='blind-box-purchase'
      aria-labelledby='blind-box-purchase-title'
    >
      <div className='blind-box-purchase-heading'>
        <h2 id='blind-box-purchase-title'>{t('购买盲盒')}</h2>
        <div className='blind-box-unit-price tabular-nums'>
          {props.balance ? props.balance.price_usd.toFixed(2) : '—'}{' '}
          <span>credits {t('/ 个')}</span>
        </div>
      </div>
      <BalanceBoxQuantityControl
        label={t('购买数量')}
        count={props.count}
        max={props.maxCount}
        disabled={props.busy}
        onChange={props.onCountChange}
      />
      <dl className='blind-box-purchase-facts'>
        <div>
          <dt>{t('今日剩余')}</dt>
          <dd>
            {props.balance?.remaining_purchase_limit ?? '—'} {t('个')}
          </dd>
        </div>
        <div>
          <dt>{t('购买后库存')}</dt>
          <dd>
            {props.balance ? props.balance.inventory_count + props.count : '—'}{' '}
            {t('个')}
          </dd>
        </div>
      </dl>
      {props.cashMethods.length > 0 ? (
        <fieldset className='blind-box-cash-methods'>
          <legend>{t('现金渠道')}</legend>
          <CashMethodPicker {...props} />
        </fieldset>
      ) : null}
      <div className='blind-box-payment-actions'>
        <PurchaseButton
          busy={props.busy && !props.cashPaying}
          disabled={!props.canPurchase}
          icon={Coins}
          title={t('额度支付')}
          amount={
            totalPrice === null ? '—' : `${totalPrice.toFixed(2)} credits`
          }
          busyLabel={t('支付中…')}
          onClick={props.onPurchase}
        />
        {props.cashMethods.length > 0 ? (
          <PurchaseButton
            variant='outline'
            busy={props.cashPaying}
            disabled={
              !props.selectedCashMethod ||
              props.busy ||
              cashLimitReached ||
              props.cashAmountDue === null ||
              !props.balance?.enabled
            }
            icon={CreditCard}
            title={t('现金支付')}
            amount={
              props.cashAmountDue === null
                ? '—'
                : `¥${props.cashAmountDue.toFixed(2)}`
            }
            busyLabel={t('下单中…')}
            onClick={props.onCashPurchase}
          />
        ) : null}
      </div>
      {walletShortfall > 0 ? (
        <p className='blind-box-shortfall' role='status'>
          {t('差 {{param0}}', {
            param0: `${walletShortfall.toFixed(2)} credits`,
          })}
        </p>
      ) : null}
    </section>
  )
}

function CashMethodPicker(props: BalanceBoxPanelViewProps) {
  return (
    <div className='blind-box-cash-method-options'>
      {props.cashMethods.map((method) => {
        const selected = props.selectedCashMethod?.type === method.type
        return (
          <button
            key={method.type}
            type='button'
            aria-pressed={selected}
            onClick={() => props.onCashMethodChange(method)}
            disabled={props.busy}
            className={cn('blind-box-cash-method', selected && 'is-selected')}
          >
            <span>{method.name}</span>
            {selected ? (
              <Check className='size-4 shrink-0' aria-hidden />
            ) : null}
          </button>
        )
      })}
    </div>
  )
}

function PurchaseButton(props: {
  busy: boolean
  disabled: boolean
  icon: typeof Coins
  title: string
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
      className='blind-box-pay-button'
      disabled={props.disabled}
      onClick={props.onClick}
    >
      <span className='flex min-w-0 items-center gap-2'>
        {props.busy ? (
          <Loader2 className='size-4 shrink-0 animate-spin' aria-hidden />
        ) : (
          <Icon className='size-4 shrink-0' aria-hidden />
        )}
        <span>{props.busy ? props.busyLabel : props.title}</span>
      </span>
      <span className='tabular-nums'>{props.amount}</span>
    </Button>
  )
}
