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
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { FileCheck2, XCircle } from 'lucide-react'
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { getAdminInvoiceRequests, updateAdminInvoiceRequest } from '../api'
import type {
  InvoiceDeliveryMethod,
  InvoiceRequest,
  InvoiceStatus,
  UpdateInvoiceRequestPayload,
} from '../types'
import { InvoiceStatusBadge } from './invoice-requests-table'

type AdminDraft = UpdateInvoiceRequestPayload

const emptyDraft = (): AdminDraft => ({
  status: 'issued',
  invoice_number: '',
  delivery_method: 'email',
  document_url: '',
  admin_note: '',
})

function formatTime(timestamp: number) {
  return timestamp
    ? new Intl.DateTimeFormat('zh-CN', {
        dateStyle: 'short',
        timeStyle: 'short',
      }).format(timestamp * 1000)
    : '-'
}

export function InvoiceAdminPanel() {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<InvoiceStatus | 'all'>('pending')
  const [selected, setSelected] = useState<InvoiceRequest | null>(null)
  const [draft, setDraft] = useState<AdminDraft>(emptyDraft)
  const requests = useQuery({
    queryKey: ['admin-invoice-requests', status],
    queryFn: () => getAdminInvoiceRequests(status),
  })
  const update = useMutation({
    mutationFn: () => updateAdminInvoiceRequest(selected!.id, draft),
    onSuccess: () => {
      toast.success(draft.status === 'issued' ? '已登记开票信息' : '已驳回申请')
      setSelected(null)
      void queryClient.invalidateQueries({
        queryKey: ['admin-invoice-requests'],
      })
      void queryClient.invalidateQueries({ queryKey: ['invoice-requests'] })
      void queryClient.invalidateQueries({
        queryKey: ['invoice-eligible-orders'],
      })
    },
    onError: () => toast.error('处理失败，请检查开票信息'),
  })

  const openReview = (request: InvoiceRequest) => {
    setSelected(request)
    setDraft({
      status: 'issued',
      invoice_number: request.invoice_number,
      delivery_method: request.delivery_method || 'email',
      document_url: request.document_url,
      admin_note: request.admin_note,
    })
  }
  const updateDraft = <K extends keyof AdminDraft>(
    key: K,
    value: AdminDraft[K]
  ) => setDraft((current) => ({ ...current, [key]: value }))
  const isIssued = draft.status === 'issued'

  return (
    <section className='border-border mt-8 border-t pt-6'>
      <div className='mb-3 flex flex-col justify-between gap-3 sm:flex-row sm:items-center'>
        <div>
          <h2 className='text-base font-semibold'>发票审核</h2>
          <p className='text-muted-foreground mt-1 text-sm'>
            仅管理员可查看抬头、税号和接收邮箱。
          </p>
        </div>
        <NativeSelect
          value={status}
          onChange={(event) =>
            setStatus(event.target.value as InvoiceStatus | 'all')
          }
        >
          <option value='pending'>待处理</option>
          <option value='issued'>已开具</option>
          <option value='rejected'>已驳回</option>
          <option value='all'>全部</option>
        </NativeSelect>
      </div>

      <div className='border-border overflow-hidden rounded-lg border'>
        {requests.isLoading ? (
          <p className='text-muted-foreground py-8 text-center text-sm'>
            正在加载申请...
          </p>
        ) : null}
        {requests.isError ? (
          <p className='text-destructive py-8 text-center text-sm'>
            申请数据加载失败
          </p>
        ) : null}
        {requests.data ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>申请人 / 订单</TableHead>
                <TableHead>开票信息</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>提交时间</TableHead>
                <TableHead className='text-right'>处理</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {requests.data.items.map((request) => (
                <TableRow key={request.id}>
                  <TableCell>
                    <div className='font-medium'>
                      用户 #{request.user_id} · {request.order_title}
                    </div>
                    <div className='text-muted-foreground text-xs'>
                      {request.trade_no} · {request.currency}{' '}
                      {request.order_amount.toFixed(2)}
                    </div>
                  </TableCell>
                  <TableCell className='whitespace-normal'>
                    <div>{request.title}</div>
                    <div className='text-muted-foreground text-xs'>
                      {request.invoice_type === 'enterprise'
                        ? `${request.tax_number} · `
                        : ''}
                      {request.email}
                    </div>
                  </TableCell>
                  <TableCell>
                    <InvoiceStatusBadge status={request.status} />
                  </TableCell>
                  <TableCell>{formatTime(request.created_at)}</TableCell>
                  <TableCell className='text-right'>
                    <Button
                      size='sm'
                      variant='outline'
                      onClick={() => openReview(request)}
                    >
                      处理
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {requests.data.items.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className='text-muted-foreground h-24 text-center'
                  >
                    没有符合筛选条件的申请
                  </TableCell>
                </TableRow>
              ) : null}
            </TableBody>
          </Table>
        ) : null}
      </div>

      <Dialog
        open={Boolean(selected)}
        onOpenChange={(open) => !open && setSelected(null)}
      >
        <DialogContent className='max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>处理发票申请</DialogTitle>
            <DialogDescription>
              {selected
                ? `${selected.title} · ${selected.order_title} · ${selected.currency} ${selected.order_amount.toFixed(2)}`
                : ''}
            </DialogDescription>
          </DialogHeader>
          <div className='grid gap-3'>
            <label className='grid gap-1.5 text-sm font-medium'>
              处理结果
              <NativeSelect
                value={draft.status}
                onChange={(event) =>
                  updateDraft(
                    'status',
                    event.target.value as AdminDraft['status']
                  )
                }
                className='w-full'
              >
                <option value='issued'>已开具</option>
                <option value='rejected'>驳回</option>
              </NativeSelect>
            </label>
            {isIssued ? (
              <>
                <label className='grid gap-1.5 text-sm font-medium'>
                  发票号码
                  <Input
                    value={draft.invoice_number}
                    onChange={(event) =>
                      updateDraft('invoice_number', event.target.value)
                    }
                    maxLength={128}
                  />
                </label>
                <label className='grid gap-1.5 text-sm font-medium'>
                  发放方式
                  <NativeSelect
                    value={draft.delivery_method}
                    onChange={(event) =>
                      updateDraft(
                        'delivery_method',
                        event.target.value as InvoiceDeliveryMethod
                      )
                    }
                    className='w-full'
                  >
                    <option value='email'>发送至申请邮箱</option>
                    <option value='download'>提供下载链接</option>
                  </NativeSelect>
                </label>
                <label className='grid gap-1.5 text-sm font-medium'>
                  HTTPS 下载链接{' '}
                  <span className='text-muted-foreground font-normal'>
                    可选
                  </span>
                  <Input
                    type='url'
                    value={draft.document_url}
                    onChange={(event) =>
                      updateDraft('document_url', event.target.value)
                    }
                    placeholder='https://...'
                  />
                </label>
              </>
            ) : null}
            <label className='grid gap-1.5 text-sm font-medium'>
              {isIssued ? '内部备注' : '驳回原因'}{' '}
              {isIssued ? (
                <span className='text-muted-foreground font-normal'>可选</span>
              ) : null}
              <Textarea
                value={draft.admin_note}
                onChange={(event) =>
                  updateDraft('admin_note', event.target.value)
                }
                maxLength={1000}
                rows={3}
              />
            </label>
          </div>
          <DialogFooter showCloseButton>
            <Button
              variant={isIssued ? 'default' : 'destructive'}
              onClick={() => update.mutate()}
              disabled={update.isPending}
            >
              {isIssued ? <FileCheck2 /> : <XCircle />}
              {update.isPending
                ? '保存中...'
                : isIssued
                  ? '确认已开具'
                  : '确认驳回'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
