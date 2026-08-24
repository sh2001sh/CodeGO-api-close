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
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
  InvoiceRequest,
  InvoiceStatus,
  UpdateInvoiceRequestPayload,
} from '../types'
import { InvoiceStatusBadge } from './invoice-requests-table'

type AdminDraft = UpdateInvoiceRequestPayload

const emptyDraft = (): AdminDraft => ({
  status: 'issued',
  invoice_number: '',
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
    mutationFn: () =>
      updateAdminInvoiceRequest(
        selected!.id,
        draft.status === 'issued'
          ? {
              status: 'issued',
              invoice_number: draft.invoice_number,
              admin_note: '',
            }
          : {
              status: 'rejected',
              invoice_number: '',
              admin_note: draft.admin_note,
            }
      ),
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
  })

  const openReview = (request: InvoiceRequest) => {
    setSelected(request)
    setDraft({
      status: 'issued',
      invoice_number: request.invoice_number,
      admin_note: request.admin_note,
    })
  }
  const updateDraft = <K extends keyof AdminDraft>(
    key: K,
    value: AdminDraft[K]
  ) => setDraft((current) => ({ ...current, [key]: value }))
  const isIssued = draft.status === 'issued'
  const invoiceNumberRequired = isIssued && !draft.invoice_number.trim()
  const rejectionReasonRequired = !isIssued && !draft.admin_note.trim()

  return (
    <Card className='bg-card/95'>
      <CardHeader className='gap-3 border-b'>
        <div className='flex flex-col justify-between gap-3 sm:flex-row sm:items-start'>
          <div>
            <CardTitle>发票审核</CardTitle>
            <CardDescription className='mt-1'>
              仅管理员可查看抬头、税号和接收邮箱，并登记发票号码或驳回原因。
            </CardDescription>
          </div>
          <NativeSelect
            value={status}
            onChange={(event) =>
              setStatus(event.target.value as InvoiceStatus | 'all')
            }
            className='w-full sm:w-36'
          >
            <option value='pending'>待处理</option>
            <option value='issued'>已开具</option>
            <option value='rejected'>已驳回</option>
            <option value='all'>全部</option>
          </NativeSelect>
        </div>
      </CardHeader>
      <CardContent>
        <div className='border-border bg-background/75 overflow-hidden rounded-2xl border'>
          {requests.isLoading ? (
            <p className='text-muted-foreground py-10 text-center text-sm'>
              正在加载申请...
            </p>
          ) : null}
          {requests.isError ? (
            <p className='text-destructive py-10 text-center text-sm'>
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
                    <TableCell className='align-top whitespace-normal'>
                      <div className='font-medium'>
                        用户 #{request.user_id} · {request.order_title}
                      </div>
                      <div className='text-muted-foreground text-xs'>
                        {request.order_count > 1
                          ? `合并 ${request.order_count} 笔订单`
                          : request.trade_no}{' '}
                        · {request.currency} {request.order_amount.toFixed(2)}
                      </div>
                    </TableCell>
                    <TableCell className='align-top whitespace-normal'>
                      <div>{request.title}</div>
                      <div className='text-muted-foreground text-xs'>
                        {request.invoice_type === 'enterprise'
                          ? request.tax_number + ' · '
                          : ''}
                        {request.email}
                      </div>
                    </TableCell>
                    <TableCell className='align-top'>
                      <InvoiceStatusBadge status={request.status} />
                    </TableCell>
                    <TableCell className='align-top whitespace-normal'>
                      {formatTime(request.created_at)}
                    </TableCell>
                    <TableCell className='text-right align-top'>
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
                      className='text-muted-foreground h-28 text-center'
                    >
                      没有符合筛选条件的申请
                    </TableCell>
                  </TableRow>
                ) : null}
              </TableBody>
            </Table>
          ) : null}
        </div>
      </CardContent>

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
              <label className='grid gap-1.5 text-sm font-medium'>
                发票号码
                <Input
                  value={draft.invoice_number}
                  onChange={(event) =>
                    updateDraft('invoice_number', event.target.value)
                  }
                  maxLength={128}
                  autoFocus
                />
                <span className='text-muted-foreground font-normal'>
                  电子税务局将自动向申请邮箱发送电子发票，本站不再另行发送。
                </span>
              </label>
            ) : (
              <label className='grid gap-1.5 text-sm font-medium'>
                驳回原因{' '}
                <span className='text-destructive font-normal'>必填</span>
                <Textarea
                  value={draft.admin_note}
                  onChange={(event) =>
                    updateDraft('admin_note', event.target.value)
                  }
                  maxLength={1000}
                  rows={3}
                />
              </label>
            )}
            {invoiceNumberRequired ? (
              <p className='text-destructive text-sm' role='alert'>
                请填写发票号码后再确认开具。
              </p>
            ) : null}
            {rejectionReasonRequired ? (
              <p className='text-destructive text-sm' role='alert'>
                请填写驳回原因后再提交，用户将能够在申请记录中看到该原因。
              </p>
            ) : null}
          </div>
          <DialogFooter showCloseButton>
            <Button
              variant={isIssued ? 'default' : 'destructive'}
              onClick={() => update.mutate()}
              disabled={
                update.isPending ||
                invoiceNumberRequired ||
                rejectionReasonRequired
              }
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
    </Card>
  )
}
