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
import { api } from '@/lib/api'
import type {
  CreateInvoiceRequestPayload,
  InvoiceEligibleOrder,
  InvoiceEligibleOrdersPage,
  InvoicePage,
  InvoiceRequest,
  InvoiceStatus,
  UpdateInvoiceRequestPayload,
} from './types'

type ApiResponse<T> = { success: boolean; message?: string; data: T }

export async function getInvoiceEligibleOrders(): Promise<
  InvoiceEligibleOrder[]
> {
  const response = await api.get<ApiResponse<InvoiceEligibleOrder[]>>(
    '/api/invoices/eligible-orders'
  )
  return response.data.data
}

export async function getInvoiceEligibleOrdersPage(
  page: number,
  keyword: string,
  signal?: AbortSignal
): Promise<InvoiceEligibleOrdersPage> {
  const config = {
    params: { p: page, page_size: 50, keyword },
    signal,
    skipErrorHandler: true,
    // React Query owns deduplication and cancellation for this user-scoped read.
    disableDuplicate: true,
  }
  const response = await api.get<ApiResponse<InvoiceEligibleOrdersPage>>(
    '/api/invoices/eligible-orders',
    config
  )
  if (!response.data.success) {
    throw new Error(response.data.message || 'invoice.ordersFailed')
  }
  return response.data.data
}

export async function getInvoiceRequests(
  signal?: AbortSignal
): Promise<InvoicePage> {
  const config = {
    params: { p: 1, page_size: 100 },
    signal,
    skipErrorHandler: true,
    disableDuplicate: true,
  }
  const response = await api.get<ApiResponse<InvoicePage>>(
    '/api/invoices/requests',
    config
  )
  return response.data.data
}

export async function createInvoiceRequest(
  payload: CreateInvoiceRequestPayload
): Promise<InvoiceRequest> {
  const response = await api.post<ApiResponse<InvoiceRequest>>(
    '/api/invoices/requests',
    payload
  )
  return response.data.data
}

export async function getAdminInvoiceRequests(
  status: InvoiceStatus | 'all'
): Promise<InvoicePage> {
  const response = await api.get<ApiResponse<InvoicePage>>(
    '/api/invoices/admin/requests',
    { params: { p: 1, page_size: 100, status: status === 'all' ? '' : status } }
  )
  return response.data.data
}

export async function updateAdminInvoiceRequest(
  id: number,
  payload: UpdateInvoiceRequestPayload
): Promise<InvoiceRequest> {
  const response = await api.put<ApiResponse<InvoiceRequest>>(
    `/api/invoices/admin/requests/${id}`,
    payload
  )
  return response.data.data
}
