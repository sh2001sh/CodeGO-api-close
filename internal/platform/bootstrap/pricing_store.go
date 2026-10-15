package bootstrap

import gatewaystore "github.com/sh2001sh/new-api/internal/gateway/store"

func loadBootstrapPricing() {
	gatewaystore.LoadPricing()
}
