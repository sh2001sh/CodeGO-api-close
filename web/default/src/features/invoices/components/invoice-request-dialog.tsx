/*
Copyright (C) 2026 codego-api contributors

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

*/
import { useEffect, useMemo, useState } from 'react'
import {
  Building2,
  CalendarDays,
  CircleAlert,
  Mail,
  ReceiptText,
  UserRound,
} from 'lucide-react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { createInvoiceRequest } from '../api'
import type {
  CreateInvoiceRequestPayload,
  InvoiceEligibleOrder,
} from '../types'

type InvoiceRequestDialogProps = {
  order: InvoiceEligibleOrder | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onSubmitted: () => void
}

const emptyForm = (): CreateInvoiceRequestPayload => ({
  source_type: 'topup',
  trade_no: '',
  invoice_type: 'personal',
  title: '',
  tax_number: '',
  email: '',
  remark: '',
})

function formatMoney(order: InvoiceEligibleOrder) {
  return order.currency + ' ' + order.order_amount.toFixed(2)
}

function formatDate(timestamp: number) {
  return timestamp
    ? new Intl.DateTimeFormat('zh-CN', {
        dateStyle: 'medium',
      }).format(timestamp * 1000)
    : '-'
}

function getOrderSourceLabel(sourceType: InvoiceEligibleOrder['source_type']) {
  return sourceType === 'subscription' ? '套餐订单' : '充值订单'
}

export function InvoiceRequestDialog({
  order,
  open,
  onOpenChange,
  onSubmitted,
}: InvoiceRequestDialogProps) {
  const [form, setForm] = useState<CreateInvoiceRequestPayload>(emptyForm)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!order || !open) return
    setForm({
      ...emptyForm(),
      source_type: order.source_type,
      trade_no: order.trade_no,
    })
  }, [open, order])

  const update = <K extends keyof CreateInvoiceRequestPayload>(
    key: K,
    value: CreateInvoiceRequestPayload[K]
  ) => setForm((current) => ({ ...current, [key]: value }))

  const submit = async () => {
    if (!order) return
    setSubmitting(true)
    try {
      await createInvoiceRequest(form)
      toast.success('发票申请已提交')
      onOpenChange(false)
      onSubmitted()
    } catch {
      toast.error('提交失败，请核对信息后重试')
    } finally {
      setSubmitting(false)
    }
  }

  const isEnterprise = form.invoice_type === 'enterprise'
  const submitDisabled = useMemo(() => {
    if (!order) return true
    if (!form.title.trim() || !form.email.trim()) return true
    if (isEnterprise && !form.tax_number.trim()) return true
    return submitting
  }, [form.email, form.tax_number, form.title, isEnterprise, order, submitting])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[calc(100vh-2rem)] overflow-y-auto p-0 sm:max-w-2xl'>
        <DialogHeader className='border-b px-5 pt-5 pb-4 sm:px-6'>
          <DialogTitle>申请电子发票</DialogTitle>
          <DialogDescription>
            {order
              ? '确认订单后填写开票信息，提交后可在发票页持续跟踪处理状态。'
              : '请选择订单'}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4 px-5 py-4 sm:px-6'>
          {order ? (
            <div className='bg-background/75 border-border/80 rounded-2xl border px-4 py-4'>
              <div className='flex flex-wrap items-start justify-between gap-3'>
                <div className='space-y-1'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <h3 className='font-medium'>{order.order_title}</h3>
                    <Badge variant='outline'>
                      {getOrderSourceLabel(order.source_type)}
                    </Badge>
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    本次申请将绑定到这笔订单，提交后不可改绑到其他订单。
                  </p>
                </div>
                <div className='text-right'>
                  <div className='text-lg font-semibold'>
                    {formatMoney(order)}
                  </div>
                  <div className='text-muted-foreground mt-1 text-xs'>
                    {order.trade_no}
                  </div>
                </div>
              </div>
              <div className='text-muted-foreground mt-4 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm'>
                <span className='inline-flex items-center gap-1.5'>
                  <CalendarDays className='size-4' />
                  {formatDate(order.paid_at)}
                </span>
                <span className='inline-flex items-center gap-1.5'>
                  <ReceiptText className='size-4' />
                  已支付订单
                </span>
              </div>
            </div>
          ) : null}

          <div className='space-y-3'>
            <div className='space-y-1'>
              <h3 className='text-sm font-medium'>开票类型</h3>
              <p className='text-muted-foreground text-sm'>
                个人发票只需抬头和接收邮箱；企业发票需要补充纳税人识别号。
              </p>
            </div>
            <div className='grid gap-3 sm:grid-cols-2'>
              <button
                type='button'
                onClick={() => update('invoice_type', 'personal')}
                className={
                  form.invoice_type === 'personal'
                    ? 'border-primary bg-primary/8 ring-primary/15 rounded-2xl border px-4 py-4 text-left ring-3'
                    : 'bg-background/75 border-border/80 hover:bg-muted/30 rounded-2xl border px-4 py-4 text-left transition-colors'
                }
              >
                <div className='flex items-start gap-3'>
                  <div className='bg-primary/10 text-primary flex size-10 items-center justify-center rounded-xl'>
                    <UserRound className='size-5' />
                  </div>
                  <div className='space-y-1'>
                    <div className='font-medium'>个人</div>
                    <p className='text-muted-foreground text-sm leading-6'>
                      适合个人报销或个人名义开具。
                    </p>
                  </div>
                </div>
              </button>
              <button
                type='button'
                onClick={() => update('invoice_type', 'enterprise')}
                className={
                  form.invoice_type === 'enterprise'
                    ? 'border-primary bg-primary/8 ring-primary/15 rounded-2xl border px-4 py-4 text-left ring-3'
                    : 'bg-background/75 border-border/80 hover:bg-muted/30 rounded-2xl border px-4 py-4 text-left transition-colors'
                }
              >
                <div className='flex items-start gap-3'>
                  <div className='bg-primary/10 text-primary flex size-10 items-center justify-center rounded-xl'>
                    <Building2 className='size-5' />
                  </div>
                  <div className='space-y-1'>
                    <div className='font-medium'>企业</div>
                    <p className='text-muted-foreground text-sm leading-6'>
                      适合公司主体开票，需要填写企业抬头和税号。
                    </p>
                  </div>
                </div>
              </button>
            </div>
          </div>

          <div className='bg-background/75 border-border/80 space-y-4 rounded-2xl border px-4 py-4'>
            <div className='space-y-1'>
              <h3 className='text-sm font-medium'>开票信息</h3>
              <p className='text-muted-foreground text-sm'>
                请确保抬头、税号和接收邮箱准确，开票后将按这些信息发放。
              </p>
            </div>

            <div className='grid gap-4 sm:grid-cols-2'>
              <label className='grid gap-1.5 text-sm font-medium'>
                发票抬头
                <Input
                  value={form.title}
                  onChange={(event) => update('title', event.target.value)}
                  placeholder={isEnterprise ? '企业全称' : '个人姓名'}
                  maxLength={255}
                />
              </label>

              <label className='grid gap-1.5 text-sm font-medium'>
                接收邮箱
                <Input
                  type='email'
                  value={form.email}
                  onChange={(event) => update('email', event.target.value)}
                  placeholder='name@example.com'
                  maxLength={255}
                />
              </label>

              {isEnterprise ? (
                <label className='grid gap-1.5 text-sm font-medium sm:col-span-2'>
                  纳税人识别号
                  <Input
                    value={form.tax_number}
                    onChange={(event) =>
                      update('tax_number', event.target.value)
                    }
                    placeholder='统一社会信用代码或纳税人识别号'
                    maxLength={64}
                  />
                </label>
              ) : null}

              <label className='grid gap-1.5 text-sm font-medium sm:col-span-2'>
                备注{' '}
                <span className='text-muted-foreground font-normal'>可选</span>
                <Textarea
                  value={form.remark}
                  onChange={(event) => update('remark', event.target.value)}
                  placeholder='如有特殊开票需求，请在此说明'
                  maxLength={500}
                  rows={4}
                />
              </label>
            </div>
          </div>

          <div className='bg-muted/35 border-border/80 text-muted-foreground flex items-start gap-3 rounded-2xl border px-4 py-3 text-sm leading-6'>
            <CircleAlert className='mt-0.5 size-4 shrink-0' />
            <div className='space-y-1'>
              <p className='text-foreground font-medium'>交付说明</p>
              <p>
                发票开具后，电子税务局会自动发送到接收邮箱；本站不另行发送邮件或提供下载链接。每笔订单仅可申请一次。
              </p>
              <p className='inline-flex items-center gap-1.5 text-xs'>
                <Mail className='size-3.5' />
                请优先填写常用工作邮箱，避免后续收件失败。
              </p>
            </div>
          </div>
        </div>

        <DialogFooter showCloseButton className='px-5 sm:px-6'>
          <Button onClick={submit} disabled={submitDisabled}>
            {submitting ? '提交中...' : '提交申请'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
