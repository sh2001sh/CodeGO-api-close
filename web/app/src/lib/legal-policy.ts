export const currentPolicyVersion = '2026-10-07'

export type PolicyDocument = 'terms' | 'privacy' | 'supplier'

export function hasCurrentPolicyAcceptance(
  records: readonly { document: string; version: string }[],
  document: PolicyDocument,
  version = currentPolicyVersion,
): boolean {
  return records.some((record) => record.document === document && record.version === version)
}
