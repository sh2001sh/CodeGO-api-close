package desktop

import (
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/legacy/identitydto"
)

func protocolScopes(r *http.Request, scopes []string) []string {
	out := append([]string{}, scopes...)
	if !identitydto.IsV3(r) {
		for i, scope := range out {
			out[i] = "desktop:" + scope
		}
	}
	return out
}

var authPermissions = []string{
	"View Code Go balance and account summary",
	"Read usage logs and trends",
	"Create and manage desktop tokens",
	"Read configuration templates for supported tools",
	"Apply desktop import and local tool configuration changes",
	"Revoke this desktop device later from profile security settings",
}
