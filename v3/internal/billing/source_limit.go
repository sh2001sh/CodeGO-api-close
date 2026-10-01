package billing

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// Model caps constrain a subscription source, never pay for the request.
// Their counters live on the same hot account and share its lifecycle fence.
type sourceLimit struct {
	SubscriptionID int64  `json:"subscription_id,omitempty"`
	Limit          int64  `json:"limit,omitempty"`
	Used           int64  `json:"used,omitempty"`
	ModelKey       string `json:"model_key,omitempty"`
	ExpiresMillis  int64  `json:"expires_millis,omitempty"`
}

func sourceLimits(profile catalog.AccountProfile, model string) map[int64]sourceLimit {
	digest := sha256.Sum256([]byte(model))
	key := "model:" + hex.EncodeToString(digest[:])
	limits := make(map[int64]sourceLimit, len(profile.Subscriptions))
	for _, bucket := range profile.Subscriptions {
		limits[bucket.AccountID] = sourceLimit{SubscriptionID: bucket.SubscriptionID, Limit: bucket.ModelLimits[model], Used: bucket.ModelUsage[model], ModelKey: key, ExpiresMillis: bucket.ExpiresAt.UnixMilli()}
	}
	return limits
}
