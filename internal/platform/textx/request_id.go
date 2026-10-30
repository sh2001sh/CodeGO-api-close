package textx

import (
	"fmt"
	"regexp"
	"strings"
)

var trailingRequestIDs = regexp.MustCompile(`(?i)(?:\s*\(request id:\s*[^()\r\n]+\))+\s*$`)

// MessageWithRequestID keeps one locally traceable identifier even when an
// upstream compatibility proxy has already appended one or more identifiers.
func MessageWithRequestID(message string, id string) string {
	message = strings.TrimSpace(trailingRequestIDs.ReplaceAllString(message, ""))
	id = strings.TrimSpace(id)
	if id == "" {
		return message
	}
	if message == "" {
		return fmt.Sprintf("(request id: %s)", id)
	}
	return fmt.Sprintf("%s (request id: %s)", message, id)
}
