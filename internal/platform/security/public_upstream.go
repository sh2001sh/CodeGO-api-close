package security

import (
	"net"
	"net/netip"
)

var publicUpstreamIPv6 = netip.MustParsePrefix("2000::/3")
var upstream6To4 = netip.MustParsePrefix("2002::/16")

// IsPublicUpstreamIP excludes reserved networks and IPv6 tunnels from
// destinations supplied by untrusted marketplace owners.
func IsPublicUpstreamIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if address.Is6() && (!publicUpstreamIPv6.Contains(address) || upstream6To4.Contains(address)) {
		return false
	}
	return ip.IsGlobalUnicast() && (&SSRFProtection{}).IsIPAccessAllowed(ip)
}
