package live

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

func (h *Handler) validatePolicy(principal gateway.Principal, model string, r *http.Request) error {
	if err := gateway.ValidateRequestPolicy(principal, model, r, h.cfg.TrustedProxies); err != nil {
		return err
	}
	if principal.Group != "" && principal.Group != "auto" && principal.AllowedGroups != nil && !slices.Contains(principal.AllowedGroups, principal.Group) {
		return errors.New("group is not permitted by this account")
	}
	if h.cfg.ValidateRequest != nil {
		return h.cfg.ValidateRequest(principal, model, r)
	}
	return nil
}

func (h *Handler) trustedProxy(address netip.Addr) bool {
	for _, prefix := range h.cfg.TrustedProxies {
		if prefix.Contains(address.Unmap()) {
			return true
		}
	}
	return false
}

func (h *Handler) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || !h.trustedProxy(peer) {
		return host
	}
	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	if len(forwarded) == 1 && strings.TrimSpace(forwarded[0]) == "" {
		forwarded[0] = r.Header.Get("X-Real-IP")
	}
	for i := len(forwarded) - 1; i >= 0; i-- {
		address, err := netip.ParseAddr(strings.TrimSpace(forwarded[i]))
		if err != nil {
			return host
		}
		if !h.trustedProxy(address) || i == 0 {
			return address.Unmap().String()
		}
	}
	return host
}
