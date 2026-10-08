package main

import (
	"net"
	"net/http"
	"net/netip"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

// Normalize once so login, device limits and control API key CIDRs all use
// the same verified client address. Incoming request state is left intact.
func clientAddressHandler(next http.Handler, trusted []netip.Prefix) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		address := gateway.ClientAddress(r, trusted)
		clone := r.Clone(r.Context())
		clone.RemoteAddr = net.JoinHostPort(address, "0")
		next.ServeHTTP(w, clone)
	})
}
