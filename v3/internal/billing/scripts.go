// Package billing reserves and settles credits in Redis for the gateway
// (plan §3 Lua #1/#3, §5). Every balance change is emitted, in the same
// atomic script, as an entry on redisx.StreamBillingEvents; the ledger worker
// turns those entries into PostgreSQL ledger rows, the source of truth.
package billing

import (
	_ "embed"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/sh2001sh/new-api/v3/pkg/redisx"
)

var (
	//go:embed scripts/money.lua
	moneyLua string
	//go:embed scripts/source_math.lua
	sourceMathLua string
	//go:embed scripts/source_round.lua
	sourceRoundLua string
	//go:embed scripts/source_allocate.lua
	sourceAllocateLua string
	//go:embed scripts/source_reserve.lua
	sourceReserveLua string
	//go:embed scripts/source_finalize.lua
	sourceFinalizeLua string
	//go:embed scripts/reserve.lua
	reserveLua string
	//go:embed scripts/finalize.lua
	finalizeLua string
	//go:embed scripts/sweep.lua
	sweepLua string
	//go:embed scripts/load.lua
	loadLua string
	//go:embed scripts/funding_reserve.lua
	fundingReserveLua string
	//go:embed scripts/funding_finalize.lua
	fundingFinalizeLua string
	//go:embed scripts/posting_reserve.lua
	postingReserveLua string

	// NewScript runs EVALSHA and falls back to EVAL on NOSCRIPT.
	reserveScript         = redis.NewScript(moneyLua + reserveLua)
	finalizeScript        = redis.NewScript(moneyLua + finalizeLua)
	sweepScript           = redis.NewScript(moneyLua + sweepLua)
	loadScript            = redis.NewScript(loadLua)
	fundingReserveScript  = redis.NewScript(moneyLua + sourceMathLua + sourceRoundLua + sourceAllocateLua + sourceReserveLua + fundingReserveLua)
	fundingFinalizeScript = redis.NewScript(moneyLua + sourceMathLua + sourceRoundLua + sourceAllocateLua + sourceFinalizeLua + fundingFinalizeLua)
	postingReserveScript  = redis.NewScript(moneyLua + postingReserveLua)
)

// Reserve script result codes.
const (
	codeReserved     = 1
	codeInsufficient = -1
	codeNotLoaded    = -2
	codeFinalized    = -3
)

// Event field names on redisx.StreamBillingEvents. The ledger worker (M2)
// reads exactly these; amounts are micro-credits in base 10.
const (
	FieldRequestID          = "request_id"
	FieldAccountID          = "account_id"
	FieldUserID             = "user_id"
	FieldKeyID              = "key_id"
	FieldModel              = "model"
	FieldChannelID          = "channel_id"
	FieldCredentialID       = "credential_id"
	FieldTerminal           = "terminal"
	FieldPromptTokens       = "prompt_tokens"
	FieldOutputTokens       = "completion_tokens"
	FieldCachedTokens       = "cached_tokens"
	FieldCacheWriteTokens   = "cache_write_tokens"
	FieldCacheWrite1hTokens = "cache_write_1h_tokens"
	FieldImageInputTokens   = "image_input_tokens"
	FieldImageOutputTokens  = "image_output_tokens"
	FieldAudioInputTokens   = "audio_input_tokens"
	FieldAudioOutputTokens  = "audio_output_tokens"
	FieldToolCalls          = "tool_calls"
	FieldEstimated          = "estimated"
	FieldAmount             = "amount"   // charged; 0 for a release
	FieldReserved           = "reserved" // amount that was held (added by the script)
	FieldOverdraft          = "overdraft"
	FieldBalanceLoaded      = "balance_loaded" // 0: balance hash was missing, ledger must apply the charge
	FieldTimestamp          = "ts"             // unix ms
	FieldCardID             = "prop_id"
	FieldCardBefore         = "before_micro"
	FieldCardAfter          = "after_micro"
)

// keys are the Redis keys of one reservation. All but the open index and the
// stream share the account's hash tag.
type keys struct {
	balance, reservation, done, member, holds string
}

// BalanceKey is the Redis hash holding an account's hot balance: fields
// "balance", "reserved" and "ver". "ver" counts non-zero charges applied in
// Redis and starts from the ledger version when the hash is loaded, so it is
// directly comparable with v3_billing.accounts.version.
func BalanceKey(accountID int64) string {
	return redisx.KeyBalancePrefix + "{" + strconv.FormatInt(accountID, 10) + "}"
}

// ReservationIndexKey indexes an account's open reservation hashes for atomic
// reserved reconstruction. Only prefixes in pkg/redisx are used for keys.
func ReservationIndexKey(accountID int64) string { return BalanceKey(accountID) + ":holds" }

func keysFor(accountID int64, requestID string) keys {
	acct := strconv.FormatInt(accountID, 10)
	rsv := redisx.KeyReservationPrefix + "{" + acct + "}:" + requestID
	return keys{
		balance:     redisx.KeyBalancePrefix + "{" + acct + "}",
		reservation: rsv,
		done:        rsv + ":done",
		member:      acct + ":" + requestID,
		holds:       ReservationIndexKey(accountID),
	}
}
