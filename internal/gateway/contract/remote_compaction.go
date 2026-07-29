package contract

import (
	"net/http"
	"strings"
)

const RemoteCompactionV2Feature = "remote_compaction_v2"

// HasRemoteCompactionV2 reports whether the client opted into the v2 protocol.
func HasRemoteCompactionV2(headers http.Header) bool {
	for _, value := range headers.Values("X-Codex-Beta-Features") {
		for _, feature := range strings.Split(value, ",") {
			if strings.TrimSpace(feature) == RemoteCompactionV2Feature {
				return true
			}
		}
	}
	return false
}
