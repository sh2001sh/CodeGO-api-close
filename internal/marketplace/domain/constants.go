package domain

const (
	SourceTypeOfficial        = "official"
	SourceTypeMarketplaceUser = "marketplace_user"

	CreditPolicyOfficialDefault = "official_model_default"
	CreditPolicyUniversalOnly   = "marketplace_universal_only"

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

	TokenGroupPrefix = "market:"
)

func AcceptsTraffic(status string) bool {
	return status == LifecycleActive || status == LifecycleDegraded
}
