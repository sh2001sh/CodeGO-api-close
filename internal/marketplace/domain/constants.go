package domain

const (
	SourceTypeOfficial        = "official"
	SourceTypeMarketplaceUser = "marketplace_user"

	CreditPolicyOfficialDefault          = "official_model_default"
	CreditPolicyUniversalOnly            = "marketplace_universal_only"
	CreditPolicySubscriptionAndUniversal = "marketplace_subscription_and_universal"

	MarketplaceSubscriptionMultiplierScale = 10.0

	LifecycleDraft         = "draft"
	LifecycleVerifying     = "verifying"
	LifecyclePendingReview = "pending_review"
	LifecycleActive        = "active"
	LifecycleDegraded      = "degraded"
	LifecycleSuspended     = "suspended"
	LifecycleDisabled      = "disabled"

	VerificationNeverRun = "never_run"
	VerificationQueued   = "queued"
	VerificationRunning  = "running"
	VerificationPassed   = "passed"
	VerificationFailed   = "failed"
	VerificationExpired  = "expired"

	VisibilityPrivate  = "private"
	VisibilityUnlisted = "unlisted"
	VisibilityPublic   = "public"

	SourceLabelPending  = "pending"
	SourceLabelApproved = "approved"
	SourceLabelRejected = "rejected"

	ModelConsistencyPassed     = "passed"
	ModelConsistencyFailed     = "failed"
	ModelConsistencyQuestioned = "questionable"

	ModelVerificationPassed = "passed"
	ModelVerificationFailed = "failed"

	TokenGroupPrefix    = "market:"
	TokenAutoGroupValue = TokenGroupPrefix + "auto"
)

// SubscriptionMultiplier converts a marketplace wallet multiplier into the
// equivalent subscription multiplier using the official 1x / 0.1x baseline.
func SubscriptionMultiplier(walletMultiplier float64) float64 {
	if walletMultiplier <= 0 {
		return 0
	}
	return walletMultiplier * MarketplaceSubscriptionMultiplierScale
}

func AcceptsTraffic(status string) bool {
	return status == LifecycleActive || status == LifecycleDegraded
}
