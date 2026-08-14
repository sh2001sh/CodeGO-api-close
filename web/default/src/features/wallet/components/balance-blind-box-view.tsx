import {
  FlaskConical,
  Gift,
  Loader2,
  PackageOpen,
  Search,
  ShoppingBag,
} from 'lucide-react'
import { useReducedMotion } from 'motion/react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type {
  BalanceBlindBoxOverview,
  BlindBoxTier,
  WalletTransferRecipient,
} from '../types'
import {
  BalanceBoxMetric,
  BalanceBoxModeButton,
  BalanceBoxQuantityControl,
} from './balance-blind-box-controls'
import { BalanceBoxGiftConfirm } from './balance-blind-box-gift-confirm'
import { BalanceBoxSimulationSummary } from './balance-blind-box-simulation'
import { BoxFigure } from './blind-box-stage'

export type ActionMode = 'purchase' | 'open' | 'gift' | 'simulate'

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
  canSimulate: boolean
  confirmGift: boolean
  onModeChange: (mode: ActionMode) => void
  onCountChange: (count: number) => void
  onRecipientIdChange: (value: string) => void
  onPurchase: () => void
  onOpen: () => void
  onSimulate: () => void
  onLookup: () => void
  onGift: () => void
  onConfirmGiftChange: (open: boolean) => void
}

export function BalanceBoxPanelView(props: BalanceBoxPanelViewProps) {
  return (
    <section className='app-page-shell overflow-hidden'>
      <BalanceBoxHeader
        balance={props.balance}
        opening={props.busy && props.mode === 'open'}
      />
      <div className='space-y-4 px-4 py-4 sm:px-6 sm:py-5'>
        <BalanceBoxModeSwitch
          mode={props.mode}
          simulationActive={Boolean(props.balance?.simulation?.active)}
          onChange={props.onModeChange}
        />
        <BalanceBoxSimulationSummary simulation={props.balance?.simulation} />
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
  const rates = {
    atLeast10: minimumRewardProbability(props.balance?.tiers || [], 10),
    atLeast15: minimumRewardProbability(props.balance?.tiers || [], 15),
  }
  return (
    <div className='relative overflow-hidden border-b border-teal-500/20 bg-gradient-to-b from-teal-500/[0.10] via-teal-500/[0.035] to-transparent px-4 pt-7 pb-5 sm:px-6'>
      <div
        aria-hidden='true'
        className='pointer-events-none absolute top-4 left-1/2 size-52 -translate-x-1/2 rounded-full bg-teal-500/15 blur-3xl'
      />
      <div className='relative flex flex-col items-center gap-4'>
        <BoxFigure
          reduced={reduced}
          pending={inventoryCount > 0}
          opening={props.opening}
          tone='balance'
        />
        <div className='max-w-xl text-center'>
          <div className='text-xs font-semibold text-teal-700 dark:text-teal-300'>
            余额盲盒 · ${(props.balance?.price_usd || 15).toFixed(0)} / 个
          </div>
          <h2 className='text-foreground mt-1 text-lg font-semibold tracking-tight sm:text-xl'>
            {inventoryCount > 0
              ? `你有 ${inventoryCount} 个余额盲盒待开启`
              : '购买可持有、可转赠的余额盲盒'}
          </h2>
          <p className='text-muted-foreground mt-1.5 text-xs leading-5'>
            购买完成后仅存入库存，不会自动开启。奖励在购买时封存，余额盲盒不产生幸运号。
          </p>
          <p className='mt-1 text-[11px] leading-5 text-teal-700/90 dark:text-teal-300/90'>
            约 {formatRate(rates.atLeast10)} 获得等值 $10 及以上奖励，约{' '}
            {formatRate(rates.atLeast15)} 达到等值 $15；Claude 额度按 4
            倍价值计算，转赠不会刷新奖励或保底。
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
        label='钱包余额'
        value={`$${(props.balance?.balance_usd || 0).toFixed(2)}`}
      />
    </div>
  )
}

function BalanceBoxModeSwitch(props: {
  mode: ActionMode
  simulationActive: boolean
  onChange: (mode: ActionMode) => void
}) {
  return (
    <div
      className={`bg-muted grid rounded-lg p-1 ${props.simulationActive ? 'grid-cols-2 sm:grid-cols-4' : 'grid-cols-3'}`}
      role='tablist'
    >
      <BalanceBoxModeButton
        active={props.mode === 'purchase'}
        icon={ShoppingBag}
        label='购买盲盒'
        onClick={() => props.onChange('purchase')}
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
      {props.simulationActive ? (
        <BalanceBoxModeButton
          active={props.mode === 'simulate'}
          icon={FlaskConical}
          label='模拟抽取'
          onClick={() => props.onChange('simulate')}
        />
      ) : null}
    </div>
  )
}

function BalanceBoxWorkspace(props: BalanceBoxPanelViewProps) {
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
  if (props.mode === 'simulate') {
    return (
      <p className='text-muted-foreground text-sm leading-6'>
        单次最多 100
        抽，总次数不限。所有结果仅用于概率测试，到期自动关闭且不会改变真实余额、道具、库存或保底。
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
    <div className='space-y-4 rounded-xl border border-teal-500/25 bg-teal-500/[0.045] p-4'>
      <BalanceBoxQuantityControl
        count={props.count}
        max={props.maxCount}
        disabled={props.busy}
        onChange={props.onCountChange}
      />
      {props.mode === 'purchase' ? (
        <BalanceBoxPurchaseAction {...props} />
      ) : null}
      {props.mode === 'simulate' ? (
        <BalanceBoxPrimaryButton
          busy={props.busy}
          disabled={!props.canSimulate}
          icon={FlaskConical}
          busyLabel='模拟中…'
          label={`模拟抽取 ${props.count} 次`}
          onClick={props.onSimulate}
        />
      ) : null}
      {props.mode !== 'purchase' && props.mode !== 'simulate' ? (
        <BalanceBoxInventoryAction {...props} />
      ) : null}
    </div>
  )
}

function BalanceBoxPurchaseAction(props: BalanceBoxPanelViewProps) {
  const totalPrice = (props.balance?.price_usd || 15) * props.count
  return (
    <div className='space-y-3 border-t border-teal-500/20 pt-3'>
      <div className='flex items-end justify-between gap-3'>
        <div>
          <div className='text-muted-foreground text-[11px]'>应付金额</div>
          <div className='text-foreground font-mono text-2xl font-semibold tabular-nums'>
            ${totalPrice.toFixed(2)}
          </div>
        </div>
        <span className='text-muted-foreground max-w-28 text-right text-[11px] leading-4'>
          付款后存入库存
        </span>
      </div>
      <BalanceBoxPrimaryButton
        busy={props.busy}
        disabled={!props.canPurchase}
        icon={ShoppingBag}
        busyLabel='购买中…'
        label='购买并存入库存'
        onClick={props.onPurchase}
      />
    </div>
  )
}

function BalanceBoxInventoryAction(props: BalanceBoxPanelViewProps) {
  const gifting = props.mode === 'gift'
  return (
    <BalanceBoxPrimaryButton
      busy={props.busy}
      disabled={
        !props.canUseInventory || (gifting && !Boolean(props.recipient))
      }
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
      className='w-full bg-teal-600 text-white hover:bg-teal-700 dark:bg-teal-500 dark:hover:bg-teal-600'
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

function minimumRewardProbability(tiers: BlindBoxTier[], threshold: number) {
  return tiers
    .filter((tier) => {
      if (tier.reward_type === 'quota') return tier.min_usd >= threshold
      if (tier.reward_type === 'claude_quota') {
        return tier.min_usd * 4 >= threshold
      }
      return false
    })
    .reduce((total, tier) => total + tier.probability, 0)
}

function formatRate(probability: number) {
  return `${(probability * 100).toFixed(1).replace(/\.0$/, '')}%`
}
