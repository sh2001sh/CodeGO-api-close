import { api, APIError } from '../../lib/api'
import type { Schema } from '../../lib/types'

export type InvoiceBuyer = Schema['OrderInvoiceBuyerInput']
export type InvoiceCorrection = Schema['OrderInvoiceCorrectionInput']

/** PDF downloads use the authenticated cookie, with the same refresh flow as JSON requests. */
export async function downloadOrderInvoice(trade: string, buyer?: InvoiceBuyer): Promise<void> {
  const path = `/api/commerce/orders/${encodeURIComponent(trade)}/invoice`
  return downloadPDF(path, buyer)
}

export function downloadInvoiceDocument(trade: string, number: string): Promise<void> {
  return downloadPDF(
    `/api/commerce/orders/${encodeURIComponent(trade)}/invoice?number=${encodeURIComponent(number)}`,
  )
}

export function correctOrderInvoice(trade: string, input: InvoiceCorrection): Promise<void> {
  return downloadPDF(`/api/commerce/orders/${encodeURIComponent(trade)}/invoice/corrections`, input)
}

export function downloadCreditNote(trade: string): Promise<void> {
  return downloadPDF(
    `/api/commerce/orders/${encodeURIComponent(trade)}/credit-note`,
    undefined,
    'POST',
  )
}

async function downloadPDF(path: string, body?: InvoiceBuyer | InvoiceCorrection, method?: string) {
  const request = () =>
    fetch(path, {
      method: method ?? (body ? 'POST' : 'GET'),
      credentials: 'same-origin',
      headers: {
        Accept: 'application/pdf',
        'X-CodeGo-API-Version': '3',
        ...(body ? { 'Content-Type': 'application/json' } : {}),
      },
      ...(body ? { body: JSON.stringify(body) } : {}),
    })
  let response = await request()
  if (response.status === 401) {
    const refreshed = await api.POST('/api/user/refresh', { body: {} })
    if (refreshed.response.ok) response = await request()
  }
  if (!response.ok) {
    let message = `HTTP ${response.status}`
    try {
      const body: unknown = await response.json()
      if (body && typeof body === 'object' && 'message' in body && typeof body.message === 'string')
        message = body.message
    } catch {
      message = '发票下载失败，请稍后重试'
    }
    throw new APIError(message, response.status)
  }
  if (response.headers.get('Content-Type')?.split(';')[0].trim() !== 'application/pdf')
    throw new APIError('服务器未返回有效的 PDF 发票', response.status)
  const blob = await response.blob()
  if (blob.size === 0) throw new APIError('服务器未返回有效的 PDF 发票', response.status)
  const filename = response.headers
    .get('Content-Disposition')
    ?.match(/\b(CG-[A-Za-z0-9-]+\.pdf)\b/)?.[1]
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename ?? 'CodeGo-invoice.pdf'
  document.body.appendChild(link)
  link.click()
  link.remove()
  // Let the browser start consuming the Blob before releasing its object URL.
  setTimeout(() => URL.revokeObjectURL(url), 1_000)
}
