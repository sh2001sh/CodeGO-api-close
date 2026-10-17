package app

import (
	"context"
	"errors"

	platformhttpx "github.com/sh2001sh/new-api/internal/platform/httpx"
)

var marketplaceUpstreamHTTPClient = platformhttpx.NewMarketplaceHTTPClient(nil, nil)

// Do not expose URL query strings, DNS answers, certificates or redirect targets
// from net/http's wrapped errors in API errors or stored verification summaries.
func marketplaceUpstreamRequestError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, platformhttpx.ErrMarketplaceUpstreamAddress) {
		return platformhttpx.ErrMarketplaceUpstreamAddress
	}
	return errors.New("上游请求失败")
}
