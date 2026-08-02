package app

var blindBoxUsageHook func(userID int, quota int)

// RegisterBlindBoxUsageHook supplies commerce-owned progression without coupling billing to commerce.
func RegisterBlindBoxUsageHook(hook func(userID int, quota int)) {
	blindBoxUsageHook = hook
}

func recordBlindBoxUsage(userID int, quota int) {
	if blindBoxUsageHook != nil && quota > 0 {
		blindBoxUsageHook(userID, quota)
	}
}
