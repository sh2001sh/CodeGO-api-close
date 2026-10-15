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
export type InvoiceSourceType = 'topup' | 'subscription'
export type InvoiceType = 'personal' | 'enterprise'
export type InvoiceStatus = 'pending' | 'issued' | 'rejected'

export type InvoiceEligibleOrder = {
  source_type: InvoiceSourceType
  trade_no: string
  order_title: string
  order_amount: number
  currency: string
  paid_at: number
  requested: boolean
}

export type InvoiceRequest = {
  id: number
  user_id: number
  source_type: InvoiceSourceType | 'batch'
  trade_no: string
  order_amount: number
  currency: string
  order_title: string
  order_count: number
  invoice_type: InvoiceType
  title: string
  tax_number: string
  email: string
  remark: string
  status: InvoiceStatus
  invoice_number: string
  admin_note: string
  handled_by: number
  issued_at: number
  created_at: number
  updated_at: number
}

export type InvoicePage = {
  items: InvoiceRequest[]
  total: number
  page: number
  page_size: number
}

export type CreateInvoiceRequestPayload = {
  orders: Array<Pick<InvoiceEligibleOrder, 'source_type' | 'trade_no'>>
  source_type: InvoiceSourceType
  trade_no: string
  invoice_type: InvoiceType
  title: string
  tax_number: string
  email: string
  remark: string
}

export type UpdateInvoiceRequestPayload = {
  status: Extract<InvoiceStatus, 'issued' | 'rejected'>
  invoice_number: string
  admin_note: string
}
