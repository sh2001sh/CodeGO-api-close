package runtime

import (
	"net"
	"net/url"
	"strings"

	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
)

// NormalizeDesktopServerAddress returns the desktop-visible server base URL.
func NormalizeDesktopServerAddress(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = strings.TrimSpace(platformconfig.ServerAddress)
	}
	if base == "" {
		base = "http://localhost:3000"
	}
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(base, "/v1")
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return strings.TrimRight(base, "/")
	}

	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if host != "localhost" && host != "127.0.0.1" && host != "0.0.0.0" && ip == nil {
			parsed.Scheme = "https"
		}
	}
	return strings.TrimRight(parsed.String(), "/")
}
