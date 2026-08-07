package runtime

// TryStartFaultDomainLastResortProbe reserves one cooling fault domain for a
// real request when every route-pool candidate is cooling. The lease keeps a
// shared upstream from receiving a concurrent recovery burst.
func TryStartFaultDomainLastResortProbe(domain, model string) bool {
	return tryStartFaultDomainProbe(domain, model, 1, false)
}

// TryStartFaultDomainEmergencyRetryProbe admits one additional bounded probe
// slot for a transient retry without reopening normal domain traffic.
func TryStartFaultDomainEmergencyRetryProbe(domain, model string) bool {
	return tryStartFaultDomainProbe(domain, model, channelHealthEmergencyProbeSlots, false)
}
