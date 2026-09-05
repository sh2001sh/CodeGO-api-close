package runtime

import (
	"sync"
	"time"
)

// FirstByteTrace separates gateway work from the wait for the first upstream
// stream event. It deliberately stores no request content or upstream identity.
type FirstByteTrace struct {
	mu                          sync.RWMutex
	startedAt                   time.Time
	bodyReadStartAt             time.Time
	bodyReadDoneAt              time.Time
	jsonDecodeStartAt           time.Time
	jsonDecodeDoneAt            time.Time
	requestValidAt              time.Time
	admittedAt                  time.Time
	relayInfoGenerationStartAt  time.Time
	relayInfoGenerationDoneAt   time.Time
	relayInfoReadyAt            time.Time
	preflightDoneAt             time.Time
	routeSelectedAt             time.Time
	channelMetaStartAt          time.Time
	channelMetaDoneAt           time.Time
	promptSensitiveStartAt      time.Time
	promptSensitiveDoneAt       time.Time
	promptAuditStartAt          time.Time
	promptAuditDoneAt           time.Time
	channelAdmissionStartAt     time.Time
	channelAdmissionDoneAt      time.Time
	faultDomainAdmissionStartAt time.Time
	faultDomainAdmissionDoneAt  time.Time
	modelPriceStartAt           time.Time
	modelPriceDoneAt            time.Time
	requestBodyRestoreStartAt   time.Time
	requestBodyRestoreDoneAt    time.Time
	billingReserveStartAt       time.Time
	billingReserveDoneAt        time.Time
	upstreamStartAt             time.Time
	requestConversionDoneAt     time.Time
	upstreamRequestReadyAt      time.Time
	upstreamResponseHeadersAt   time.Time
	firstEventAt                time.Time
	firstSemanticReadAt         time.Time
	firstSemanticAt             time.Time
	firstTextReadAt             time.Time
	firstTextAt                 time.Time
	firstHeadersFlushAt         time.Time
	firstFlushAt                time.Time
	firstSemanticIsText         bool
	semanticKindMarked          bool
	outboundTraceMarked         bool
	outboundDNSDuration         time.Duration
	outboundConnectDuration     time.Duration
	outboundTLSDuration         time.Duration
	outboundGotConnAt           time.Time
	outboundWroteRequestAt      time.Time
	outboundFirstByteAt         time.Time
	outboundConnReused          bool
	outboundConnWasIdle         bool
	outboundConnIdleDuration    time.Duration
	outboundConnMarked          bool
	outboundRequestBodyBytes    int64
	outboundHTTPProtoMajor      int64
	outboundHTTPProtoMinor      int64
	requestBodyFastPath         bool
}

// FirstByteTraceContextKey stores the request trace in the Gin context so
// lower-level HTTP and streaming packages can add timing marks without
// importing the relay handler package.
const FirstByteTraceContextKey = "gateway_first_byte_trace"

func NewFirstByteTrace(startedAt time.Time) *FirstByteTrace {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &FirstByteTrace{startedAt: startedAt}
}

func (t *FirstByteTrace) MarkBodyReadStarted() { t.mark(&t.bodyReadStartAt) }
func (t *FirstByteTrace) MarkBodyReadDone()    { t.mark(&t.bodyReadDoneAt) }

// BodyReadDoneTime returns the instant the client request body was fully
// materialized by the gateway. It is used as the origin for latency metrics
// that should exclude downstream upload time.
func (t *FirstByteTrace) BodyReadDoneTime() time.Time {
	if t == nil {
		return time.Time{}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.bodyReadDoneAt
}
func (t *FirstByteTrace) MarkJSONDecodeStarted() { t.mark(&t.jsonDecodeStartAt) }
func (t *FirstByteTrace) MarkJSONDecodeDone()    { t.mark(&t.jsonDecodeDoneAt) }
func (t *FirstByteTrace) MarkHeadersFlush()      { t.mark(&t.firstHeadersFlushAt) }
func (t *FirstByteTrace) MarkFirstFlush()        { t.mark(&t.firstFlushAt) }

func (t *FirstByteTrace) MarkRequestValidated()            { t.mark(&t.requestValidAt) }
func (t *FirstByteTrace) MarkAdmitted()                    { t.mark(&t.admittedAt) }
func (t *FirstByteTrace) MarkRelayInfoGenerationStarted()  { t.mark(&t.relayInfoGenerationStartAt) }
func (t *FirstByteTrace) MarkRelayInfoGenerationDone()     { t.mark(&t.relayInfoGenerationDoneAt) }
func (t *FirstByteTrace) MarkRelayInfoReady()              { t.mark(&t.relayInfoReadyAt) }
func (t *FirstByteTrace) MarkPreflightDone()               { t.mark(&t.preflightDoneAt) }
func (t *FirstByteTrace) MarkRouteSelected()               { t.mark(&t.routeSelectedAt) }
func (t *FirstByteTrace) MarkChannelMetaStarted()          { t.mark(&t.channelMetaStartAt) }
func (t *FirstByteTrace) MarkChannelMetaDone()             { t.mark(&t.channelMetaDoneAt) }
func (t *FirstByteTrace) MarkPromptSensitiveStarted()      { t.mark(&t.promptSensitiveStartAt) }
func (t *FirstByteTrace) MarkPromptSensitiveDone()         { t.mark(&t.promptSensitiveDoneAt) }
func (t *FirstByteTrace) MarkPromptAuditStarted()          { t.mark(&t.promptAuditStartAt) }
func (t *FirstByteTrace) MarkPromptAuditDone()             { t.mark(&t.promptAuditDoneAt) }
func (t *FirstByteTrace) MarkChannelAdmissionStarted()     { t.mark(&t.channelAdmissionStartAt) }
func (t *FirstByteTrace) MarkChannelAdmissionDone()        { t.mark(&t.channelAdmissionDoneAt) }
func (t *FirstByteTrace) MarkFaultDomainAdmissionStarted() { t.mark(&t.faultDomainAdmissionStartAt) }
func (t *FirstByteTrace) MarkFaultDomainAdmissionDone()    { t.mark(&t.faultDomainAdmissionDoneAt) }
func (t *FirstByteTrace) MarkModelPriceStarted()           { t.mark(&t.modelPriceStartAt) }
func (t *FirstByteTrace) MarkModelPriceDone()              { t.mark(&t.modelPriceDoneAt) }
func (t *FirstByteTrace) MarkRequestBodyRestoreStarted() {
	t.mark(&t.requestBodyRestoreStartAt)
}
func (t *FirstByteTrace) MarkRequestBodyRestoreDone() { t.mark(&t.requestBodyRestoreDoneAt) }
func (t *FirstByteTrace) MarkBillingReserveStarted()  { t.mark(&t.billingReserveStartAt) }
func (t *FirstByteTrace) MarkBillingReserveDone()     { t.mark(&t.billingReserveDoneAt) }
func (t *FirstByteTrace) MarkUpstreamStart()          { t.mark(&t.upstreamStartAt) }
func (t *FirstByteTrace) MarkRequestConversionDone() {
	t.mark(&t.requestConversionDoneAt)
}
func (t *FirstByteTrace) MarkUpstreamRequestReady() {
	t.mark(&t.upstreamRequestReadyAt)
}
func (t *FirstByteTrace) MarkUpstreamResponseHeaders() {
	t.mark(&t.upstreamResponseHeadersAt)
}
func (t *FirstByteTrace) MarkFirstEvent() { t.mark(&t.firstEventAt) }
func (t *FirstByteTrace) MarkFirstSemanticEvent() {
	t.mark(&t.firstSemanticAt)
}

// MarkFirstSemanticReadAt records when the scanner received the first
// model-visible SSE frame, before handler scheduling and JSON decoding.
func (t *FirstByteTrace) MarkFirstSemanticReadAt(receivedAt time.Time, isText bool) {
	if t == nil || receivedAt.IsZero() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstSemanticReadAt.IsZero() {
		t.firstSemanticReadAt = receivedAt
		t.firstSemanticIsText = isText
		t.semanticKindMarked = true
	}
}

// MarkFirstTextReadAt records when the scanner received the first visible
// output-text delta, excluding reasoning and lifecycle frames.
func (t *FirstByteTrace) MarkFirstTextReadAt(receivedAt time.Time) {
	if t == nil || receivedAt.IsZero() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.firstTextReadAt.IsZero() {
		t.firstTextReadAt = receivedAt
	}
}

func (t *FirstByteTrace) MarkFirstTextEvent() {
	t.mark(&t.firstTextAt)
}

// MarkRequestBodyFastPath records that the validated request body was reused
// without protocol conversion or JSON re-marshalling.
func (t *FirstByteTrace) MarkRequestBodyFastPath() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.requestBodyFastPath = true
	t.mu.Unlock()
}

func (t *FirstByteTrace) mark(target *time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if target.IsZero() {
		*target = time.Now()
	}
}

// Snapshot returns millisecond durations suitable for operational logs.
func (t *FirstByteTrace) Snapshot() map[string]int64 {
	return t.snapshot(false, time.Time{})
}

// ProgressSnapshot returns completed stages even when a request failed before
// semantic output. The current elapsed values make no-output failures
// diagnosable without recording request content or credentials.
func (t *FirstByteTrace) ProgressSnapshot(now time.Time) map[string]int64 {
	return t.snapshot(true, now)
}

func (t *FirstByteTrace) snapshot(includeProgress bool, now time.Time) map[string]int64 {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.firstSemanticAt.IsZero() && !includeProgress {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	snapshot := map[string]int64{
		"ingress_ms":                       durationMilliseconds(t.startedAt, t.relayInfoReadyAt),
		"request_validation_ms":            durationMilliseconds(t.startedAt, t.requestValidAt),
		"admission_ms":                     durationMilliseconds(t.requestValidAt, t.admittedAt),
		"relay_info_ms":                    durationMilliseconds(t.admittedAt, t.relayInfoReadyAt),
		"preflight_ms":                     durationMilliseconds(t.relayInfoReadyAt, t.preflightDoneAt),
		"route_selection_ms":               durationMilliseconds(t.preflightDoneAt, t.routeSelectedAt),
		"dispatch_ms":                      durationMilliseconds(t.routeSelectedAt, t.upstreamStartAt),
		"upstream_first_event_ms":          durationMilliseconds(t.upstreamStartAt, t.firstEventAt),
		"event_to_semantic_ms":             durationMilliseconds(t.firstEventAt, t.firstSemanticAt),
		"upstream_first_semantic_read_ms":  durationMilliseconds(t.upstreamStartAt, t.firstSemanticReadAt),
		"semantic_read_to_handler_ms":      durationMilliseconds(t.firstSemanticReadAt, t.firstSemanticAt),
		"upstream_first_semantic_event_ms": durationMilliseconds(t.upstreamStartAt, t.firstSemanticAt),
		"total_raw_event_ms":               durationMilliseconds(t.startedAt, t.firstEventAt),
		"total_ms":                         durationMilliseconds(t.startedAt, t.firstSemanticAt),
	}
	if !t.bodyReadStartAt.IsZero() && !t.bodyReadDoneAt.IsZero() {
		snapshot["body_receive_ms"] = durationMilliseconds(t.bodyReadStartAt, t.bodyReadDoneAt)
		snapshot["body_receive_from_request_start_ms"] = durationMilliseconds(t.startedAt, t.bodyReadDoneAt)
	}
	if !t.jsonDecodeStartAt.IsZero() && !t.jsonDecodeDoneAt.IsZero() {
		snapshot["json_decode_ms"] = durationMilliseconds(t.jsonDecodeStartAt, t.jsonDecodeDoneAt)
	}
	if !t.firstFlushAt.IsZero() {
		snapshot["first_flush_ms"] = durationMilliseconds(t.startedAt, t.firstFlushAt)
		if !t.firstSemanticAt.IsZero() {
			snapshot["semantic_to_first_flush_ms"] = durationMilliseconds(t.firstSemanticAt, t.firstFlushAt)
		}
	}
	if !t.firstHeadersFlushAt.IsZero() {
		snapshot["headers_flush_ms"] = durationMilliseconds(t.startedAt, t.firstHeadersFlushAt)
	}
	if !t.requestBodyRestoreStartAt.IsZero() && !t.requestBodyRestoreDoneAt.IsZero() {
		snapshot["request_body_restore_ms"] = durationMilliseconds(t.requestBodyRestoreStartAt, t.requestBodyRestoreDoneAt)
	}
	if !t.relayInfoGenerationStartAt.IsZero() && !t.relayInfoGenerationDoneAt.IsZero() {
		snapshot["relay_info_generation_ms"] = durationMilliseconds(t.relayInfoGenerationStartAt, t.relayInfoGenerationDoneAt)
	}
	if !t.channelMetaStartAt.IsZero() && !t.channelMetaDoneAt.IsZero() {
		snapshot["channel_meta_init_ms"] = durationMilliseconds(t.channelMetaStartAt, t.channelMetaDoneAt)
	}
	if !t.promptSensitiveStartAt.IsZero() && !t.promptSensitiveDoneAt.IsZero() {
		snapshot["prompt_sensitive_check_ms"] = durationMilliseconds(t.promptSensitiveStartAt, t.promptSensitiveDoneAt)
	}
	if !t.promptAuditStartAt.IsZero() && !t.promptAuditDoneAt.IsZero() {
		snapshot["prompt_audit_ms"] = durationMilliseconds(t.promptAuditStartAt, t.promptAuditDoneAt)
	}
	if !t.channelAdmissionStartAt.IsZero() && !t.channelAdmissionDoneAt.IsZero() {
		snapshot["channel_admission_ms"] = durationMilliseconds(t.channelAdmissionStartAt, t.channelAdmissionDoneAt)
	}
	if !t.faultDomainAdmissionStartAt.IsZero() && !t.faultDomainAdmissionDoneAt.IsZero() {
		snapshot["fault_domain_admission_ms"] = durationMilliseconds(t.faultDomainAdmissionStartAt, t.faultDomainAdmissionDoneAt)
	}
	if !t.modelPriceStartAt.IsZero() && !t.modelPriceDoneAt.IsZero() {
		snapshot["model_price_ms"] = durationMilliseconds(t.modelPriceStartAt, t.modelPriceDoneAt)
	}
	if !t.billingReserveStartAt.IsZero() && !t.billingReserveDoneAt.IsZero() {
		snapshot["billing_reservation_ms"] = durationMilliseconds(t.billingReserveStartAt, t.billingReserveDoneAt)
	}
	if !t.upstreamStartAt.IsZero() && !t.requestConversionDoneAt.IsZero() {
		snapshot["request_conversion_ms"] = durationMilliseconds(t.upstreamStartAt, t.requestConversionDoneAt)
	}
	if !t.requestConversionDoneAt.IsZero() && !t.upstreamRequestReadyAt.IsZero() {
		snapshot["upstream_request_setup_ms"] = durationMilliseconds(t.requestConversionDoneAt, t.upstreamRequestReadyAt)
	}
	if !t.upstreamRequestReadyAt.IsZero() && !t.upstreamResponseHeadersAt.IsZero() {
		snapshot["upstream_response_headers_ms"] = durationMilliseconds(t.upstreamRequestReadyAt, t.upstreamResponseHeadersAt)
	}
	if !t.upstreamResponseHeadersAt.IsZero() && !t.firstEventAt.IsZero() {
		snapshot["headers_to_first_event_ms"] = durationMilliseconds(t.upstreamResponseHeadersAt, t.firstEventAt)
	}
	if t.outboundTraceMarked {
		snapshot["outbound_dns_ms"] = t.outboundDNSDuration.Milliseconds()
		snapshot["outbound_connect_ms"] = t.outboundConnectDuration.Milliseconds()
		snapshot["outbound_tls_ms"] = t.outboundTLSDuration.Milliseconds()
		snapshot["outbound_request_body_bytes"] = t.outboundRequestBodyBytes
	}
	if t.outboundConnMarked {
		snapshot["outbound_conn_reused"] = boolToInt64(t.outboundConnReused)
		snapshot["outbound_conn_was_idle"] = boolToInt64(t.outboundConnWasIdle)
		snapshot["outbound_conn_idle_ms"] = t.outboundConnIdleDuration.Milliseconds()
		snapshot["outbound_conn_wait_ms"] = durationMilliseconds(t.upstreamRequestReadyAt, t.outboundGotConnAt)
	}
	if !t.outboundGotConnAt.IsZero() && !t.outboundWroteRequestAt.IsZero() {
		snapshot["outbound_request_write_ms"] = durationMilliseconds(t.outboundGotConnAt, t.outboundWroteRequestAt)
	}
	if !t.outboundWroteRequestAt.IsZero() && !t.outboundFirstByteAt.IsZero() {
		snapshot["upstream_server_wait_ms"] = durationMilliseconds(t.outboundWroteRequestAt, t.outboundFirstByteAt)
	}
	if !t.upstreamRequestReadyAt.IsZero() && !t.outboundFirstByteAt.IsZero() {
		snapshot["outbound_first_response_byte_ms"] = durationMilliseconds(t.upstreamRequestReadyAt, t.outboundFirstByteAt)
	}
	if !t.outboundFirstByteAt.IsZero() && !t.upstreamResponseHeadersAt.IsZero() {
		snapshot["outbound_response_header_decode_ms"] = durationMilliseconds(t.outboundFirstByteAt, t.upstreamResponseHeadersAt)
	}
	if t.outboundHTTPProtoMajor > 0 {
		snapshot["outbound_http_proto_major"] = t.outboundHTTPProtoMajor
		snapshot["outbound_http_proto_minor"] = t.outboundHTTPProtoMinor
	}
	if t.requestBodyFastPath {
		snapshot["request_body_fast_path"] = 1
	}
	if t.semanticKindMarked {
		if t.firstSemanticIsText {
			snapshot["first_semantic_is_text"] = 1
		} else {
			snapshot["first_semantic_is_text"] = 0
		}
	}
	if !t.firstTextReadAt.IsZero() {
		snapshot["upstream_first_text_read_ms"] = durationMilliseconds(t.upstreamStartAt, t.firstTextReadAt)
		snapshot["text_read_to_handler_ms"] = durationMilliseconds(t.firstTextReadAt, t.firstTextAt)
	}
	if includeProgress {
		snapshot["elapsed_ms"] = durationMilliseconds(t.startedAt, now)
		if !t.upstreamStartAt.IsZero() {
			upstreamEnd := t.firstSemanticAt
			if upstreamEnd.IsZero() {
				upstreamEnd = now
			}
			snapshot["upstream_elapsed_ms"] = durationMilliseconds(t.upstreamStartAt, upstreamEnd)
		}
	}
	return snapshot
}

func durationMilliseconds(start, end time.Time) int64 {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
