package auxiliary

import (
	"errors"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (h *Handler) rejectRequest(w http.ResponseWriter, req *gateway.Request, err error) {
	var upstream *gateway.UpstreamError
	if !errors.As(err, &upstream) {
		upstream = failure(502, "upstream_failure", "upstream request failed")
	}
	gateway.RecordRequest(h.cfg.Requests, req, gateway.Outcome{Err: upstream}, false)
	writeError(w, err)
}
