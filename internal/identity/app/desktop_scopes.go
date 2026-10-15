package app

import (
	"strings"

	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
)

var desktopDefaultScopes = []string{
	identitydomain.DesktopScopeAccountRead,
	identitydomain.DesktopScopeLogsRead,
	identitydomain.DesktopScopeTokensRead,
	identitydomain.DesktopScopeTokensWrite,
	identitydomain.DesktopScopeConfigRead,
	identitydomain.DesktopScopeConfigWrite,
	identitydomain.DesktopScopeTelemetryWrite,
}

func DefaultDesktopScopes() []string {
	scopes := make([]string, len(desktopDefaultScopes))
	copy(scopes, desktopDefaultScopes)
	return scopes
}

func NormalizeDesktopScopes(scopes []string) []string {
	if len(scopes) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(scopes))
	normalized := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		value := strings.TrimSpace(scope)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func SerializeDesktopScopes(scopes []string) string {
	return strings.Join(NormalizeDesktopScopes(scopes), ",")
}

func ParseDesktopScopes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return NormalizeDesktopScopes(strings.Split(raw, ","))
}

func DesktopDeviceHasScope(device *identitydomain.DesktopAuthorizedDevice, required string) bool {
	if device == nil {
		return false
	}
	scope := strings.TrimSpace(required)
	if scope == "" {
		return true
	}

	scopes := ParseDesktopScopes(device.Scopes)
	if len(scopes) == 0 {
		// Legacy devices authorized before scopes were persisted keep full access
		// until they are re-approved with explicit grants.
		return true
	}
	for _, item := range scopes {
		if item == scope {
			return true
		}
	}
	if scope == identitydomain.DesktopScopeTelemetryWrite {
		for _, item := range scopes {
			if item == identitydomain.DesktopScopeConfigWrite {
				return true
			}
		}
	}
	return false
}
