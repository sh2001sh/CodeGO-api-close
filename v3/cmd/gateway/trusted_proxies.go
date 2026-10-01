package main

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
)

func loadTrustedProxies() ([]netip.Prefix, error) {
	value := strings.TrimSpace(os.Getenv("V3_TRUSTED_PROXY_CIDRS"))
	if value == "" {
		return nil, nil
	}
	var prefixes []netip.Prefix
	for _, value := range strings.Split(value, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("V3_TRUSTED_PROXY_CIDRS: %w", err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}
