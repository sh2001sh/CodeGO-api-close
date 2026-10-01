package redisx

// Redis key and channel names shared across v3 modules. Keys that one Lua
// script touches together share a {hash tag} so they live on one cluster slot.
const (
	// Concurrency ZSET members are request ids, scored by lease expiry.
	// Scripts touching these keys require one Redis primary.
	KeyConcurrencyPrefix    = "v3:concurrency:"
	KeyUserRPMPrefix        = "v3:rpm:user:"
	KeyUserRPMRequestPrefix = "v3:rpm:request:"
	// ChannelInvalidate carries cache invalidations published by the outbox
	// worker. Payload: "<entity>:<entity_id>", e.g. "api_key:42", "catalog:7".
	ChannelInvalidate = "v3:invalidate"

	// ChannelSnapshot announces a new catalog snapshot. Payload: decimal version.
	ChannelSnapshot = "v3:snapshot"

	// KeySnapshotPrefix + version holds a compiled snapshot blob (JSON).
	KeySnapshotPrefix = "v3:snapshot:"

	// KeyAPIKeyPrefix + hex(sha256(key)) caches an authorized key profile (L2).
	KeyAPIKeyPrefix = "v3:apikey:"

	// KeyAPIKeyUserPrefix + userID is the set of L2 key hashes owned by a user,
	// so a user invalidation can drop every cached key of that user.
	KeyAPIKeyUserPrefix = "v3:apikey:user:"

	// KeyAPIKeyIDPrefix + keyID maps a key id to its L2 hash, so an api_key
	// invalidation (which carries only the id) can find the entry.
	KeyAPIKeyIDPrefix = "v3:apikey:id:"

	// KeyAPIKeyTombPrefix + "user:<id>" or "key:<id>" holds the Redis time (ms)
	// of the last invalidation. An L2 fill whose PostgreSQL read started
	// before that time is discarded, so a load racing an invalidation cannot
	// write stale data back.
	KeyAPIKeyTombPrefix = "v3:apikey:tomb:"

	// KeyBalancePrefix + "{" + accountID + "}" is the hot balance hash:
	// fields "balance" and "reserved" (micro-credits), "ver" (ledger version).
	KeyBalancePrefix = "v3:bal:"

	// KeyReservationPrefix + "{" + accountID + "}:" + requestID holds an open
	// reservation: fields "amount", "expires". The same key + ":done" marks a
	// finalized request so retries of finalize are no-ops.
	KeyReservationPrefix = "v3:rsv:"

	// KeyReservationOpen is a ZSET of open reservations, member
	// "<accountID>:<requestID>", score = expiry in unix ms. The sweeper uses it
	// to release reservations whose gateway died before finalizing. Billing
	// scripts update it atomically with the balance, which assumes a single
	// Redis primary (not Redis Cluster).
	KeyReservationOpen = "v3:rsv:open"
	// KeyPostingOpen tracks business debit holds until their PG transaction
	// either commits an outbox entry or rolls back.
	KeyPostingOpen = "v3:billing:posting:open"

	// StreamBillingEvents is the Redis Stream of settled usage consumed by the
	// ledger worker (consumer group GroupLedger). Entry fields: see billing.Event.
	StreamBillingEvents = "v3:billing:events"
	GroupLedger         = "ledger"
)
