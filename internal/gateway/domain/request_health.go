package domain

const (
	RequestHealthHealthy  = "healthy"
	RequestHealthUnstable = "unstable"
	RequestHealthFailed   = "failed"
	RequestHealthUnknown  = "unknown"
)

// ClassifyRequestHealth maps one request sample window to the shared health
// vocabulary used by all group-status surfaces.
func ClassifyRequestHealth(successRate float64, requestCount int64) string {
	if requestCount <= 0 {
		return RequestHealthUnknown
	}
	if successRate >= 90 {
		return RequestHealthHealthy
	}
	if successRate >= 85 {
		return RequestHealthUnstable
	}
	return RequestHealthFailed
}
