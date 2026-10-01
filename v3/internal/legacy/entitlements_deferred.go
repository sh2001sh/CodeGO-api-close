package legacy

// Reset opportunity accrual/history has no commissioned native game runtime.
// Preserve its source inventory without restoring that game. Commerce reads
// original use records independently to retain the paid financial restriction.
func deferredResetHistory(name string) bool {
	return name == "subscription_reset_opportunity_accounts" || name == "subscription_reset_opportunity_ledgers"
}
