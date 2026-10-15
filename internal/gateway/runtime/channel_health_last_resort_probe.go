package runtime

// TryStartChannelLastResortProbe reserves a cooling channel for one real
// request when a route pool has no healthy candidate left. The normal recovery
// path waits for CoolingUntil; this path retains the same short lease but may
// probe earlier, preventing an all-cooling pool from becoming a hard outage.
func TryStartChannelLastResortProbe(channelID int, model string, requestTypes ...RequestType) bool {
	return tryStartChannelProbe(channelID, model, 1, false, requestTypes...)
}
