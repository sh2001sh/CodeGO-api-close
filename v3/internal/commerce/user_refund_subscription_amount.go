package commerce

import (
	"math/big"

	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

// Refund cash is denominated in the order's original grant. An administrative
// quota change rewrites the subscription's total and usage, but never the paid
// order. Convert usage back to the paid basis before calculating the refund.
// Imported consumption proofs already use that original basis.
func subscriptionRefundRemaining(paid, total, used, current credits.Micro, origin *credits.Micro) (credits.Micro, error) {
	if paid <= 0 || total < 0 || used < 0 || current < 0 || origin != nil && *origin < 0 {
		return 0, ErrRefundUnavailable
	}
	if total == 0 {
		// Period-only subscriptions retain the order's lifetime refund basis.
		total = paid
	}
	// Supplemental grants must not make previously consumed paid principal
	// refundable again. Only a quota reduction changes the comparison scale.
	total = min(total, paid)
	consumed := scaleRefundConsumption(used, paid, total)
	if origin != nil {
		consumed = max(consumed, min(*origin, paid))
	}
	remaining := paid - consumed
	return max(remaining-scaleRefundConsumption(current, paid, total), 0), nil
}

// Round consumption upward so a micro-credit remainder cannot increase the
// refundable principal. big.Int avoids overflowing large historical grants.
func scaleRefundConsumption(amount, paid, total credits.Micro) credits.Micro {
	if amount >= total {
		return paid
	}
	n := new(big.Int).Mul(big.NewInt(int64(amount)), big.NewInt(int64(paid)))
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, big.NewInt(int64(total)), r)
	if r.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return credits.Micro(q.Int64())
}
