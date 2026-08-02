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
import {
  CheckCircle2,
  CircleSlash,
  ExternalLink,
  Gift,
  Loader2,
  QrCode,
  XCircle,
} from 'lucide-react'
import React from 'react'
import { QRCodeCanvas } from 'qrcode.react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import type { BlindBoxPaymentState } from './blind-box-dialogs'

const TIMEOUT_MS = 60000

const STATUS_CONFIG = {
  pending: {
    icon: <Loader2 className='size-5 animate-spin' />,
    title: '等待支付',
    tone: 'border-warning/20 bg-warning/5',
  },
  success: {
    icon: <CheckCircle2 className='size-5 text-success' />,
    title: '支付成功',
    tone: 'border-success/20 bg-success/5',
  },
  failed: {
    icon: <XCircle className='size-5 text-destructive' />,
    title: '支付失败',
    tone: 'border-destructive/20 bg-destructive/5',
  },
  idle: {
    icon: <CircleSlash className='size-5 text-muted-foreground' />,
    title: '待支付',
    tone: 'border-border/70 bg-background/50',
  },
} as const

export function BlindBoxPaymentDialog(props: {
  state: BlindBoxPaymentState
  onOpenChange: (open: boolean) => void
  onOpenExternal: () => void
  onContinueInBackground?: () => void
  onRetry?: () => void
}) {
  const [showExitConfirm, setShowExitConfirm] = React.useState(false)

  const handleCloseAttempt = () => {
    if (props.state.stage === 'pending') {
      setShowExitConfirm(true)
    } else {
      props.onOpenChange(false)
    }
  }

  const handleConfirmExit = () => {
    setShowExitConfirm(false)
    props.onOpenChange(false)
  }

  const handleContinueInBackground = () => {
    setShowExitConfirm(false)
    props.onContinueInBackground?.()
    props.onOpenChange(false)
  }

  const elapsedTime = props.state.pollingStartTime
    ? Date.now() - props.state.pollingStartTime
    : 0
  const isTimedOut =
    props.state.stage === 'pending' && elapsedTime > TIMEOUT_MS
  const statusConfig = STATUS_CONFIG[props.state.stage]

  return (
    <Dialog open={props.state.open} onOpenChange={handleCloseAttempt}>
      <DialogContent
        className='w-[calc(100vw-1rem)] max-w-xl overflow-hidden p-0'
        showCloseButton={true}
      >
        <DialogHeader className='border-b px-5 py-4'>
          <DialogTitle className='flex items-center gap-2 text-base'>
            <Gift className='size-5' />
            支付确认
          </DialogTitle>
        </DialogHeader>

        {showExitConfirm ? (
          <ExitConfirmPanel
            onContinueInBackground={handleContinueInBackground}
            onConfirmExit={handleConfirmExit}
            onBack={() => setShowExitConfirm(false)}
          />
        ) : (
          <div className='space-y-4 px-5 py-5'>
            <div className={cn('rounded-xl border p-4', statusConfig.tone)}>
              <div className='flex items-start gap-3'>
                <div className='border-border bg-card flex size-10 items-center justify-center rounded-full border'>
                  {statusConfig.icon}
                </div>
                <div className='min-w-0 flex-1'>
                  <div className='text-foreground text-sm font-semibold'>
                    {statusConfig.title}
                  </div>
                  <div className='text-muted-foreground mt-1 text-sm leading-6'>
                    {isTimedOut
                      ? '支付处理时间较长，你可以关闭对话框，结果会自动同步'
                      : props.state.message || '请使用下方二维码完成支付'}
                  </div>
                </div>
              </div>
            </div>

            <div className='grid gap-3 sm:grid-cols-2'>
              <Metric label='购买数量' value={String(props.state.quantity)} />
              <Metric
                label='支付金额'
                value={`¥${props.state.amountDue.toFixed(2)}`}
              />
              <Metric label='支付方式' value={props.state.methodLabel} />
              <Metric label='订单号' value={props.state.orderId || '--'} mono />
            </div>

            {props.state.stage === 'pending' ? (
              <PaymentQrPanel
                qrCodeUrl={props.state.qrCodeUrl}
                payUrl={props.state.payUrl}
              />
            ) : props.state.stage === 'failed' ? (
              <div className='app-subtle-panel p-4'>
                <div className='text-muted-foreground text-sm leading-6'>
                  {props.state.message || '支付失败'}
                </div>
                {props.onRetry && props.state.retryPayload ? (
                  <Button className='mt-3' onClick={props.onRetry}>
                    重新支付
                  </Button>
                ) : null}
              </div>
            ) : null}

            <div className='flex flex-wrap gap-2'>
              {(props.state.payUrl || props.state.formUrl) &&
              props.state.stage === 'pending' ? (
                <Button variant='outline' onClick={props.onOpenExternal}>
                  <ExternalLink data-icon='inline-start' />
                  打开支付页
                </Button>
              ) : null}
              <Button variant='ghost' onClick={handleCloseAttempt}>
                关闭
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

function ExitConfirmPanel(props: {
  onContinueInBackground: () => void
  onConfirmExit: () => void
  onBack: () => void
}) {
  return (
    <div className='space-y-4 px-5 py-5'>
      <div className='border-warning/20 bg-warning/5 rounded-xl border p-4'>
        <div className='text-foreground text-sm font-semibold'>
          支付仍在处理中
        </div>
        <div className='text-muted-foreground mt-1 text-sm leading-6'>
          关闭对话框后，支付结果会自动同步到账户。你可以在账单历史中查看订单状态。
        </div>
      </div>

      <div className='flex flex-wrap gap-2'>
        <Button onClick={props.onContinueInBackground}>后台继续</Button>
        <Button variant='outline' onClick={props.onConfirmExit}>
          关闭对话框
        </Button>
        <Button variant='ghost' onClick={props.onBack}>
          返回支付
        </Button>
      </div>
    </div>
  )
}

function PaymentQrPanel(props: { qrCodeUrl: string; payUrl: string }) {
  return (
    <div className='app-subtle-panel p-4'>
      <div className='mx-auto flex w-full max-w-[240px] flex-col items-center gap-3'>
        {props.qrCodeUrl ? (
          <div className='border-border bg-background rounded-xl border p-4'>
            <img
              src={props.qrCodeUrl}
              alt='payment-qrcode'
              className='h-48 w-48 object-contain'
            />
          </div>
        ) : props.payUrl ? (
          <div className='border-border bg-background rounded-xl border p-4'>
            <QRCodeCanvas value={props.payUrl} size={192} />
          </div>
        ) : (
          <div className='border-border/70 text-muted-foreground rounded-xl border border-dashed px-5 py-10 text-center text-sm'>
            请点击下方按钮继续支付
          </div>
        )}

        <div className='text-muted-foreground flex items-center gap-2 text-center text-xs leading-5'>
          <QrCode className='size-4 shrink-0' />
          支付完成后自动同步结果
        </div>
      </div>
    </div>
  )
}

function Metric(props: { label: string; value: string; mono?: boolean }) {
  return (
    <div className='app-subtle-panel px-3 py-3'>
      <div className='text-muted-foreground text-[11px] font-medium'>
        {props.label}
      </div>
      <div
        className={cn(
          'text-foreground mt-1 text-sm font-semibold',
          props.mono && 'break-all font-mono'
        )}
      >
        {props.value}
      </div>
    </div>
  )
}
