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

export function InvoiceRequestsTable({ requests }: InvoiceRequestsTableProps) {
  if (requests.length === 0) {
    return (
      <p className='text-muted-foreground py-8 text-center text-sm'>
        暂无发票申请记录
      </p>
    )
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>订单</TableHead>
          <TableHead>抬头</TableHead>
          <TableHead>状态</TableHead>
          <TableHead>发票号码</TableHead>
          <TableHead className='text-right'>交付</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {requests.map((request) => (
          <TableRow key={request.id}>
            <TableCell>
              <div className='font-medium'>{request.order_title}</div>
              <div className='text-muted-foreground text-xs'>
                {request.currency} {request.order_amount.toFixed(2)} ·{' '}
                {request.trade_no}
              </div>
            </TableCell>
            <TableCell>{request.title}</TableCell>
            <TableCell>
              <InvoiceStatusBadge status={request.status} />
            </TableCell>
            <TableCell>
              {request.invoice_number || (
                <span className='text-muted-foreground'>待开具</span>
              )}
              {request.status === 'rejected' && request.admin_note ? (
                <div className='text-destructive mt-1 max-w-56 text-xs whitespace-normal'>
                  {request.admin_note}
                </div>
              ) : null}
            </TableCell>
            <TableCell className='text-right'>
              {request.status === 'issued' && request.document_url ? (
                <Button
                  size='icon-sm'
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
                </Button>
              ) : request.status === 'issued' &&
                request.delivery_method === 'email' ? (
                <span className='text-muted-foreground inline-flex items-center gap-1 text-xs'>
                  <Mail className='size-3.5' />
                  邮箱发送
                </span>
              ) : (
                <span className='text-muted-foreground text-xs'>-</span>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
