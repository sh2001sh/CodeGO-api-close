import axios from 'axios'
import { api } from '@/lib/api'
import type { InvoiceEligibleOrder } from './types'

export type InvoiceBuyer = {
  buyer_name: string
  buyer_address: string
  buyer_country?: string
  buyer_tax_id?: string
}
export type InvoiceDocument = {
  number: string
  document_type: 'invoice' | 'credit_note'
  issued_at: number
  related_number?: string
  reason?: string
  buyer: InvoiceBuyer
}
export type InvoiceIssueInput = InvoiceBuyer & {
  previous_number?: string
  reason?: string
  request_id?: string
}

export class InvoiceError extends Error {
  constructor(
    public status: number,
    message: string
  ) {
    super(message)
  }
}

function path(order: InvoiceEligibleOrder) {
  return `/api/invoices/orders/${order.source_type}/${encodeURIComponent(order.trade_no)}`
}

export async function getInvoiceDocuments(
  order: InvoiceEligibleOrder,
  signal?: AbortSignal
): Promise<InvoiceDocument[]> {
  const config = {
    signal,
    skipErrorHandler: true,
    disableDuplicate: true,
  }
  const response = await api.get<{ data: InvoiceDocument[] }>(
    `${path(order)}/documents`,
    config
  )
  return response.data.data ?? []
}

export async function downloadInvoice(
  order: InvoiceEligibleOrder,
  action: 'document' | 'corrections' | 'credit-note' = 'document',
  body?: InvoiceIssueInput,
  number?: string,
  batchOrders?: InvoiceEligibleOrder[]
) {
  let response
  try {
    response = await api.request<Blob>({
      url: batchOrders
        ? '/api/invoices/batches/document'
        : `${path(order)}/${action}`,
      method: batchOrders || body || action === 'credit-note' ? 'POST' : 'GET',
      data: batchOrders
        ? {
            ...body,
            orders: batchOrders.map((item) => ({
              source_type: item.source_type,
              trade_no: item.trade_no,
            })),
          }
        : body,
      params: number ? { number } : undefined,
      responseType: 'blob',
      skipErrorHandler: true,
      headers: { Accept: 'application/pdf' },
    })
  } catch (error) {
    if (!axios.isAxiosError<Blob>(error)) throw error
    let message = 'invoice.downloadFailed'
    try {
      const data: unknown = JSON.parse(
        (await error.response?.data.text()) ?? '{}'
      )
      if (
        data &&
        typeof data === 'object' &&
        'message' in data &&
        typeof data.message === 'string'
      )
        message = data.message
    } catch {
      /* Proxy responses use the localized download error. */
    }
    throw new InvoiceError(error.response?.status ?? 0, message)
  }
  if (
    response.data.type.split(';')[0] !== 'application/pdf' ||
    response.data.size === 0
  )
    throw new InvoiceError(0, 'invoice.downloadFailed')
  const filename =
    String(response.headers['content-disposition'] ?? '').match(
      /\b(CG-[A-Za-z0-9-]+\.pdf)\b/
    )?.[1] ?? 'CodeGo-invoice.pdf'
  const url = URL.createObjectURL(response.data)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
