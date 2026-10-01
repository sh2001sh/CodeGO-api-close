package ledger

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/billing"
)

// event is one parsed entry of redisx.StreamBillingEvents.
type event struct {
	streamID    string
	requestID   string
	accountID   int64
	amount      int64 // micro-credits charged; 0 for releases and sweeps
	fingerprint [32]byte
	fields      map[string]string // the raw entry, kept as ledger metadata
}

var errMalformed = errors.New("malformed billing event")

// parseEvent validates the fields the ledger depends on.
func parseEvent(streamID string, values map[string]any) (event, error) {
	e := event{streamID: streamID, fields: make(map[string]string, len(values))}
	for k, v := range values {
		s, ok := v.(string)
		if !ok {
			return e, fmt.Errorf("%w: field %s is %T", errMalformed, k, v)
		}
		e.fields[k] = s
	}
	e.requestID = e.fields[billing.FieldRequestID]
	if e.requestID == "" {
		return e, fmt.Errorf("%w: missing %s", errMalformed, billing.FieldRequestID)
	}
	var err error
	if e.accountID, err = strconv.ParseInt(e.fields[billing.FieldAccountID], 10, 64); err != nil || e.accountID <= 0 {
		return e, fmt.Errorf("%w: bad %s %q", errMalformed, billing.FieldAccountID, e.fields[billing.FieldAccountID])
	}
	if e.amount, err = strconv.ParseInt(e.fields[billing.FieldAmount], 10, 64); err != nil || e.amount < 0 {
		return e, fmt.Errorf("%w: bad %s %q", errMalformed, billing.FieldAmount, e.fields[billing.FieldAmount])
	}
	if e.fields[billing.FieldCardID] != "" {
		prop, propErr := strconv.ParseInt(e.fields[billing.FieldCardID], 10, 64)
		channel, channelErr := strconv.ParseInt(e.fields[billing.FieldChannelID], 10, 64)
		before, beforeErr := strconv.ParseInt(e.fields[billing.FieldCardBefore], 10, 64)
		after, afterErr := strconv.ParseInt(e.fields[billing.FieldCardAfter], 10, 64)
		amount := e.amount
		if e.fields["funding_part"] == "primary" {
			if amount, err = strconv.ParseInt(e.fields["usage_total_amount"], 10, 64); err != nil {
				return e, fmt.Errorf("%w: bad aggregate card amount", errMalformed)
			}
		}
		if propErr != nil || channelErr != nil || beforeErr != nil || afterErr != nil || prop <= 0 || channel <= 0 ||
			before <= after || after < 0 || amount != after || e.fields["funding_part"] == "secondary" {
			return e, fmt.Errorf("%w: inconsistent consumption-card metadata", errMalformed)
		}
	} else if e.fields[billing.FieldCardBefore] != "" || e.fields[billing.FieldCardAfter] != "" {
		return e, fmt.Errorf("%w: card amounts without prop ID", errMalformed)
	}
	e.fingerprint = fingerprint(e.fields)
	return e, nil
}

// fingerprint hashes every field in a canonical order. A redelivered entry
// has the same fields, so the same fingerprint; a different charge for the
// same request does not.
func fingerprint(fields map[string]string) [32]byte {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		_, _ = fmt.Fprintf(h, "%d:%s%d:%s", len(k), k, len(fields[k]), fields[k])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
