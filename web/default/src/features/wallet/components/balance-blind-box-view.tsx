import { useState } from 'react'
import i18n from '@/i18n/config'
import {
  Gift,
  Loader2,
  PackageOpen,
  Search,
  ShieldCheck,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation, Trans } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import type {
  BalanceBlindBoxOverview,
  PaymentMethod,
  WalletTransferRecipient,
} from '../types'
import { BalanceBoxQuantityControl } from './balance-blind-box-controls'
import { BalanceBoxGiftConfirm } from './balance-blind-box-gift-confirm'
import { BalanceBoxPurchaseWorkspace } from './balance-blind-box-purchase'
import { EditorialBoxGlyph } from './blind-box-editorial-glyph'
import { BlindBoxPityTrack } from './blind-box-rules-panel'

export type ActionMode = 'purchase' | 'open' | 'gift' | 'pity'

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
  cashAmountDue: number | null
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
  const { t } = useTranslation()
  const [confirmPurchase, setConfirmPurchase] = useState(false)
  return (
    <section className='blind-box-operation overflow-hidden'>
      <BalanceBoxHeader
        balance={props.balance}
        canOpen={props.canUseInventory}
        opening={props.busy && props.mode === 'open'}
        busy={props.busy}
        onOpenCount={props.onOpenCount}
        mode={props.mode}
        onModeChange={props.onModeChange}
      />
      <div className='blind-box-purchase-surface'>
        <BalanceBoxPurchaseWorkspace
          {...props}
          onPurchase={() => setConfirmPurchase(true)}
        />
        <BalanceBoxSecondaryPanel {...props} />
      </div>
      <BalanceBoxGiftConfirm {...props} count={props.inventoryActionCount} />
      <Dialog open={confirmPurchase} onOpenChange={setConfirmPurchase}>
        <DialogContent className='commerce-dialog commerce-workspace sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('确认购买')}</DialogTitle>
          </DialogHeader>
          <dl className='divide-y border-y'>
            <div className='flex justify-between gap-6 py-4'>
              <dt className='text-muted-foreground'>{t('数量')}</dt>
              <dd>{props.count}</dd>
            </div>
            <div className='flex justify-between gap-6 py-4'>
              <dt className='text-muted-foreground'>{t('单价')}</dt>
              <dd>{props.balance?.price_usd.toFixed(2) ?? '—'} credits</dd>
            </div>
            <div className='flex justify-between gap-6 py-4'>
              <dt className='text-muted-foreground'>{t('订单合计')}</dt>
              <dd className='text-primary font-semibold'>
                {props.balance
                  ? (props.balance.price_usd * props.count).toFixed(2)
                  : '—'}{' '}
                credits
              </dd>
            </div>
          </dl>
          <div className='flex justify-end gap-3'>
            <Button variant='outline' onClick={() => setConfirmPurchase(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={!props.canPurchase}
              onClick={() => {
                setConfirmPurchase(false)
                props.onPurchase()
              }}
            >
              {t('额度支付')}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </section>
  )
}

function BalanceBoxHeader(props: {
  balance?: BalanceBlindBoxOverview
  canOpen: boolean
  opening: boolean
  busy: boolean
  onOpenCount: (count: number) => void
  mode: ActionMode
  onModeChange: (mode: ActionMode) => void
}) {
  const { t } = useTranslation()
  const inventoryCount = props.balance?.inventory_count ?? 0
  const openAllCount = Math.min(100, inventoryCount)
  const ops = [
    { mode: 'pity' as ActionMode, icon: ShieldCheck, label: t('保底进度') },
    { mode: 'gift' as ActionMode, icon: Gift, label: t('赠送库存') },
  ]

  return (
    <div className='blind-box-inventory-surface'>
      <div className='blind-box-inventory-heading'>
        <div className='min-w-0'>
          <h2>{t('待开盲盒')}</h2>
          <div className='blind-box-inventory-count tabular-nums'>
            {props.balance ? inventoryCount : '—'}
            <span>{t('个')}</span>
          </div>
        </div>
        <EditorialBoxGlyph
          pending={inventoryCount > 0}
          opening={props.opening}
        />
      </div>
      <dl className='blind-box-inventory-metrics'>
        <div>
          <dt>{t('今日购买')}</dt>
          <dd className='tabular-nums'>
            {props.balance
              ? `${props.balance.purchased_today ?? '—'} / ${props.balance.daily_purchase_limit ?? '—'}`
              : '—'}
          </dd>
        </div>
        <div>
          <dt>{t('统一额度')}</dt>
          <dd className='tabular-nums'>
            {props.balance ? props.balance.balance_usd.toFixed(2) : '—'}{' '}
            <span>credits</span>
          </dd>
        </div>
      </dl>
      <div className='blind-box-open-actions'>
        <Button disabled={!props.canOpen} onClick={() => props.onOpenCount(1)}>
          {props.opening ? (
            <Loader2 className='size-4 animate-spin' aria-hidden />
          ) : (
            <PackageOpen className='size-4' aria-hidden />
          )}
          {t('开启 1 个')}
        </Button>
        <Button
          variant='outline'
          disabled={!props.canOpen}
          onClick={() => props.onOpenCount(openAllCount)}
        >
          {t('全部')} {openAllCount} {t('个')}
        </Button>
      </div>
      {inventoryCount === 0 ? (
        <p className='blind-box-inventory-empty'>{t('盲盒库存为空')}</p>
      ) : null}
      <div className='blind-box-inventory-tools'>
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
        'blind-box-inventory-tool',
        props.active ? 'is-active' : ''
      )}
    >
      <Icon className='size-3.5 shrink-0' />
      <span className='text-wrap'>{props.label}</span>
    </button>
  )
}

function BalanceBoxSecondaryPanel(props: BalanceBoxPanelViewProps) {
  if (props.mode === 'pity') {
    return (
      <div className='blind-box-secondary-panel'>
        <BlindBoxPityTrack balance={props.balance} />
      </div>
    )
  }
  if (props.mode === 'gift') {
    return (
      <div className='blind-box-secondary-panel'>
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
  useTranslation()
  return (
    <div className='space-y-2'>
      <label className='text-sm font-medium' htmlFor='balance-box-recipient'>
        <Trans i18nKey={'接收方公开 ID'} />
      </label>
      <div className='flex gap-2'>
        <Input
          id='balance-box-recipient'
          maxLength={6}
          value={props.recipientId}
          onChange={(event) =>
            props.onRecipientIdChange(event.target.value.toUpperCase())
          }
          className='font-mono uppercase'
        />
        <Button
          variant='outline'
          size='icon'
          onClick={props.onLookup}
          disabled={props.busy}
          aria-label={i18n.t('查找用户')}
        >
          <Search className='size-4' />
        </Button>
      </div>
      {props.recipient ? (
        <div className='border-border bg-muted/40 rounded-md border px-3 py-2 text-sm'>
          <Trans i18nKey={'接收方：'} />
          {props.recipient.display_name_masked} · {props.recipient.external_id}
        </div>
      ) : null}
      <p className='text-muted-foreground text-xs leading-5'>
        <Trans
          i18nKey={
            '赠送不可撤销，盲盒可被接收方继续转赠。封存奖励不会因所有权变化而重新抽取。'
          }
        />
      </p>
    </div>
  )
}

function BalanceBoxActionControls(props: BalanceBoxPanelViewProps) {
  const { t } = useTranslation()
  return (
    <div className='space-y-4'>
      <BalanceBoxQuantityControl
        label={`${t('赠送盲盒')} · ${t('盲盒数量')}`}
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
  useTranslation()

  const gifting = props.mode === 'gift'
  return (
    <BalanceBoxPrimaryButton
      busy={props.busy}
      disabled={!props.canUseInventory || (gifting && !props.recipient)}
      icon={gifting ? Gift : PackageOpen}
      busyLabel={gifting ? i18n.t('赠送中…') : i18n.t('开启中…')}
      label={i18n.t('{{param0}} {{param1}} 个', {
        param0: gifting ? i18n.t('赠送') : i18n.t('开启'),
        param1: props.inventoryActionCount,
      })}
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
