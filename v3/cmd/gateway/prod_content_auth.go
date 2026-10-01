package main

import (
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/sh2001sh/new-api/v3/cmd/internal/boot"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/identity"
)

// Browser sessions may read only their owned video content. Normal generation
// and task metadata still require API keys through the gateway authorizer.
func newContentAuthorizer(deps *boot.Deps, log *slog.Logger) (func(*http.Request) (gateway.Principal, error), error) {
	raw, present := os.LookupEnv("V3_SESSION_SECRET")
	if !present {
		return nil, nil
	}
	secret, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(secret) < 32 {
		return nil, errors.New("V3_SESSION_SECRET must be base64 with at least 32 decoded bytes")
	}
	if deps == nil || deps.PG == nil || deps.Crypto == nil {
		return nil, errors.New("video session authorization requires PostgreSQL and server encryption")
	}
	control, err := identity.NewControl(deps.PG.Pool, identity.ControlConfig{
		SessionSecret: secret, EncryptionKey: deps.Crypto.DeriveKey("gateway-content-auth"),
	}, log)
	if err != nil {
		return nil, err
	}
	return func(r *http.Request) (gateway.Principal, error) {
		user, err := control.AuthenticateRequest(r)
		if err != nil {
			return gateway.Principal{}, err
		}
		return gateway.Principal{UserID: user.ID, Group: user.Group}, nil
	}, nil
}
