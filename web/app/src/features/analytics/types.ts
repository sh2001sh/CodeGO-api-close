// Shared types and query-building helpers for the analytics workstream
// (usage logs, request/event/usage audit, billing ledger).
import type { components } from '../../lib/api.generated'

export type AuditUsage = components['schemas']['AuditUsage']
export type AuditSummary = components['schemas']['AuditSummary']
export type AuditRequestAudit = components['schemas']['AuditRequestAudit']
export type AuditAttemptAudit = components['schemas']['AuditAttemptAudit']
export type AuditEvent = components['schemas']['AuditEvent']
export type AuditSample = components['schemas']['AuditSample']
export type Balance = components['schemas']['Balance']
export type LedgerEntry = components['schemas']['LedgerEntry']
export type LedgerHistoricalEntry = components['schemas']['LedgerHistoricalEntry']

/** Filters shared by /api/log/* and /api/audit/* list endpoints. */
export type UsageFilters = {
  model: string
  userID: string
  keyID: string
  channelID: string
}

export const emptyFilters: UsageFilters = { model: '', userID: '', keyID: '', channelID: '' }

/** Only forward IDs that are plain positive integers; anything else is dropped
 * rather than sent, since the server rejects malformed integers with a 400. */
function numericID(value: string): number | undefined {
  return /^\d+$/.test(value) ? Number(value) : undefined
}

export function usageQuery(filters: UsageFilters, range: { from: string; to: string }) {
  return {
    model: filters.model || undefined,
    user_id: numericID(filters.userID),
    key_id: numericID(filters.keyID),
    channel_id: numericID(filters.channelID),
    from: range.from || undefined,
    to: range.to || undefined,
  }
}
