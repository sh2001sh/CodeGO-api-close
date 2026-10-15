package app

import (
	"strconv"
	"strings"
)

// NormalizeGeneMapWindowDays resolves the lookback window from body/query inputs.
func NormalizeGeneMapWindowDays(queryDays string, bodyDays int, fallback int) int {
	if bodyDays > 0 {
		return bodyDays
	}
	parsed, _ := strconv.Atoi(strings.TrimSpace(queryDays))
	if parsed > 0 {
		return parsed
	}
	return fallback
}

// BuildPublicGeneMapShare returns the stored public gene-map share snapshot.
func BuildPublicGeneMapShare(token string) (any, error) {
	return GetPublicGeneMapShare(token)
}
