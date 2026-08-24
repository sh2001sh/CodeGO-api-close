export type RequestHealthStatus = 'healthy' | 'unstable' | 'failed' | 'unknown'

const REQUEST_HEALTH_LABELS: Record<RequestHealthStatus, string> = {
  healthy: '稳定',
  unstable: '波动',
  failed: '异常',
  unknown: '暂无近期请求',
}

export function classifyRequestHealth(
  successRate: number | null | undefined,
  requestCount: number | null | undefined
): RequestHealthStatus {
  if (!requestCount || requestCount <= 0 || successRate == null) {
    return 'unknown'
  }
  if (successRate >= 90) return 'healthy'
  if (successRate >= 85) return 'unstable'
  return 'failed'
}

export function getRequestHealthLabel(status: RequestHealthStatus) {
  return REQUEST_HEALTH_LABELS[status]
}
