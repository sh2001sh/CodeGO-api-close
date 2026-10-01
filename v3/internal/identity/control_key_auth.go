package identity

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// AuthenticateKeyRequest reads current key and user state for the low-volume
// control API. It deliberately does not grant session or administrator access.
func (c *Control) AuthenticateKeyRequest(r *http.Request) (*KeyProfile, error) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) > 512 {
		return nil, ErrCredentials
	}
	p, err := (pgLoader{pool: c.pool}).load(r.Context(), HashKey(parts[1]))
	if err != nil {
		return nil, err
	}
	if p == nil || p.usable(c.cfg.Now()) != nil {
		return nil, ErrCredentials
	}
	if p.AllowedCIDRs != nil {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		addr, err := netip.ParseAddr(host)
		if err != nil || !p.AllowsAddr(addr) {
			return nil, ErrAddressNotAllowed
		}
	}
	return p, nil
}
