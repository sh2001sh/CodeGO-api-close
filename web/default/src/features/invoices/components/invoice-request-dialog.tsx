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
import { useEffect, useState } from 'react'
import { Building2, UserRound } from 'lucide-react'
import { toast } from 'sonner'
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
import { NativeSelect } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { createInvoiceRequest } from '../api'
import type {
  CreateInvoiceRequestPayload,
  InvoiceEligibleOrder,
  InvoiceType,
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
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>申请电子发票</DialogTitle>
          <DialogDescription>
            {order
              ? `${order.order_title} · ${order.currency} ${order.order_amount.toFixed(2)}`
              : '请选择订单'}
          </DialogDescription>
        </DialogHeader>

        <div className='grid gap-3'>
          <label className='grid gap-1.5 text-sm font-medium'>
            发票类型
            <NativeSelect
              value={form.invoice_type}
              onChange={(event) =>
                update('invoice_type', event.target.value as InvoiceType)
              }
              className='w-full'
            >
              <option value='personal'>个人</option>
              <option value='enterprise'>企业</option>
            </NativeSelect>
          </label>
          <label className='grid gap-1.5 text-sm font-medium'>
            发票抬头
            <Input
              value={form.title}
              onChange={(event) => update('title', event.target.value)}
              placeholder={isEnterprise ? '企业全称' : '个人姓名'}
              maxLength={255}
            />
          </label>
          {isEnterprise ? (
            <label className='grid gap-1.5 text-sm font-medium'>
              纳税人识别号
              <Input
                value={form.tax_number}
                onChange={(event) => update('tax_number', event.target.value)}
                placeholder='统一社会信用代码或纳税人识别号'
                maxLength={64}
              />
            </label>
          ) : null}
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
          <label className='grid gap-1.5 text-sm font-medium'>
            备注 <span className='text-muted-foreground font-normal'>可选</span>
            <Textarea
              value={form.remark}
              onChange={(event) => update('remark', event.target.value)}
              placeholder='如有特殊开票需求，请在此说明'
              maxLength={500}
              rows={3}
            />
          </label>
          <div className='text-muted-foreground flex items-start gap-2 text-xs leading-5'>
            {isEnterprise ? (
              <Building2 className='mt-0.5 size-4 shrink-0' />
            ) : (
              <UserRound className='mt-0.5 size-4 shrink-0' />
            )}
            已开具的电子发票将发送至接收邮箱；管理员也可提供 HTTPS
            下载链接。每笔订单仅可申请一次。
          </div>
        </div>

        <DialogFooter showCloseButton>
          <Button onClick={submit} disabled={submitting || !order}>
            {submitting ? '提交中...' : '提交申请'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
