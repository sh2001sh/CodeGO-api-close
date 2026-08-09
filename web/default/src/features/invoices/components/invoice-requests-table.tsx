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
import { Download, FileCheck2, FileClock, FileX2, Mail } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { InvoiceRequest, InvoiceStatus } from '../types'

type InvoiceRequestsTableProps = { requests: InvoiceRequest[] }

const statusMeta: Record<
  InvoiceStatus,
  {
    label: string
    variant: 'secondary' | 'default' | 'destructive'
    icon: typeof FileClock
  }
> = {
  pending: { label: '待处理', variant: 'secondary', icon: FileClock },
  issued: { label: '已开具', variant: 'default', icon: FileCheck2 },
  rejected: { label: '已驳回', variant: 'destructive', icon: FileX2 },
}

function formatMoney(request: InvoiceRequest) {
  return request.currency + ' ' + request.order_amount.toFixed(2)
}

function formatDateTime(timestamp: number) {
  return timestamp
    ? new Intl.DateTimeFormat('zh-CN', {
        dateStyle: 'medium',
        timeStyle: 'short',
      }).format(timestamp * 1000)
    : '-'
}

export function InvoiceStatusBadge({ status }: { status: InvoiceStatus }) {
  const meta = statusMeta[status]
  const Icon = meta.icon
  return (
    <Badge variant={meta.variant}>
      <Icon />
      {meta.label}
    </Badge>
  )
}

function DeliveryCell({ request }: { request: InvoiceRequest }) {
  if (request.status === 'issued' && request.document_url) {
    return (
      <Button
        size='sm'
        variant='outline'
        render={
          <a
            href={request.document_url}
            target='_blank'
            rel='noreferrer'
            title='下载电子发票'
          />
        }
      >
        <Download />
        下载发票
      </Button>
    )
  }

  if (request.status === 'issued' && request.delivery_method === 'email') {
    return (
      <span className='text-muted-foreground inline-flex items-center gap-1 text-xs'>
        <Mail className='size-3.5' />
        邮箱发送
      </span>
    )
  }

  return <span className='text-muted-foreground text-xs'>-</span>
}

export function InvoiceRequestsTable({ requests }: InvoiceRequestsTableProps) {
  if (requests.length === 0) {
    return (
      <div className='flex min-h-56 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed bg-muted/20 px-6 text-center'>
        <FileClock className='text-muted-foreground size-5' />
        <p className='font-medium'>暂无发票申请记录</p>
        <p className='text-muted-foreground max-w-md text-sm'>
          提交申请后，处理结果、发票号码和交付方式都会显示在这里。
        </p>
      </div>
    )
  }

  return (
    <>
      <div className='space-y-3 md:hidden'>
        {requests.map((request) => (
          <div
            key={request.id}
            className='bg-background/75 border-border/80 space-y-3 rounded-2xl border px-4 py-4'
          >
            <div className='flex flex-wrap items-start justify-between gap-2'>
              <div className='space-y-1'>
                <div className='font-medium'>{request.order_title}</div>
                <div className='text-muted-foreground text-sm'>
                  {formatMoney(request)}
                </div>
              </div>
              <InvoiceStatusBadge status={request.status} />
            </div>
            <div className='grid gap-3 text-sm sm:grid-cols-2'>
              <div>
                <div className='text-muted-foreground text-xs'>抬头</div>
                <div className='mt-1'>{request.title}</div>
              </div>
              <div>
                <div className='text-muted-foreground text-xs'>订单号</div>
                <div className='mt-1 font-mono text-xs'>{request.trade_no}</div>
              </div>
              <div>
                <div className='text-muted-foreground text-xs'>申请时间</div>
                <div className='mt-1'>{formatDateTime(request.created_at)}</div>
              </div>
              <div>
                <div className='text-muted-foreground text-xs'>发票号码</div>
                <div className='mt-1'>
                  {request.invoice_number || '待开具'}
                </div>
              </div>
            </div>
            {request.status === 'rejected' && request.admin_note ? (
              <div className='bg-destructive/8 text-destructive rounded-xl px-3 py-2 text-xs leading-5'>
                {request.admin_note}
              </div>
            ) : null}
            <div className='flex justify-end'>
              <DeliveryCell request={request} />
            </div>
          </div>
        ))}
      </div>

      <div className='hidden md:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>订单与抬头</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>申请时间</TableHead>
              <TableHead>发票号码</TableHead>
              <TableHead className='text-right'>交付</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {requests.map((request) => (
              <TableRow key={request.id}>
                <TableCell className='align-top whitespace-normal'>
                  <div className='space-y-1'>
                    <div className='font-medium'>{request.order_title}</div>
                    <div className='text-muted-foreground text-xs'>
                      {formatMoney(request)} · {request.trade_no}
                    </div>
                    <div className='text-sm'>{request.title}</div>
                  </div>
                </TableCell>
                <TableCell className='align-top'>
                  <InvoiceStatusBadge status={request.status} />
                </TableCell>
                <TableCell className='align-top whitespace-normal'>
                  {formatDateTime(request.created_at)}
                </TableCell>
                <TableCell className='align-top whitespace-normal'>
                  {request.invoice_number || (
                    <span className='text-muted-foreground'>待开具</span>
                  )}
                  {request.status === 'rejected' && request.admin_note ? (
                    <div className='text-destructive mt-1 max-w-56 text-xs leading-5'>
                      {request.admin_note}
                    </div>
                  ) : null}
                </TableCell>
                <TableCell className='text-right align-top'>
                  <DeliveryCell request={request} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </>
  )
}
