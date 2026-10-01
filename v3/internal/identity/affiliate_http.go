package identity

import (
	"math"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/legacy/identitydto"
)

func (c *Control) affiliateTransferHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := c.requireUser(w, r)
	if !ok {
		return
	}
	var body struct {
		Quota              *int64 `json:"quota"`
		AmountMicroCredits *int64 `json:"amount_micro_credits"`
		OperationID        string `json:"operation_id"`
	}
	if err := decodeControl(w, r, &body); err != nil {
		c.reply(w, nil, err)
		return
	}
	in := AffiliateTransferInput{OperationID: body.OperationID}
	if header := r.Header.Get("Idempotency-Key"); header != "" {
		if in.OperationID != "" && in.OperationID != header {
			c.reply(w, nil, ErrInvalidInput)
			return
		}
		in.OperationID = header
	}
	if identitydto.IsV3(r) {
		if body.Quota != nil || body.AmountMicroCredits == nil || in.OperationID == "" {
			c.reply(w, nil, ErrInvalidInput)
			return
		}
		in.AmountMicroCredits = *body.AmountMicroCredits
	} else {
		if body.AmountMicroCredits != nil || body.Quota == nil || *body.Quota < 500000 || *body.Quota > math.MaxInt64/2 {
			c.reply(w, nil, ErrInvalidInput)
			return
		}
		in.AmountMicroCredits = *body.Quota * 2
		// Original clients have no retry identifier. Keep that input compatible;
		// clients requesting retry safety send Idempotency-Key or operation_id.
		if in.OperationID == "" {
			var err error
			in.OperationID, err = randomToken()
			if err != nil {
				c.reply(w, nil, err)
				return
			}
		}
	}
	result, err := c.TransferAffiliate(r.Context(), u.ID, in)
	if err != nil {
		c.reply(w, nil, err)
		return
	}
	if identitydto.IsV3(r) {
		c.reply(w, result, nil)
		return
	}
	c.reply(w, nil, nil)
}
