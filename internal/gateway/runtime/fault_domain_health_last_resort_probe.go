package runtime

// TryStartFaultDomainLastResortProbe reserves one cooling fault domain for a
// real request when every route-pool candidate is cooling. The lease keeps a
// shared upstream from receiving a concurrent recovery burst.
func TryStartFaultDomainLastResortProbe(domain, model string) bool {
	return tryStartFaultDomainProbe(domain, model, 1, false)
}

// TryStartFaultDomainRateLimitRetryProbe admits one additional bounded probe
// slot for a retry after a 429 without reopening normal domain traffic.
func TryStartFaultDomainRateLimitRetryProbe(domain, model string) bool {
	return tryStartFaultDomainProbe(domain, model, channelHealthRateLimitProbeSlots, false)
}
