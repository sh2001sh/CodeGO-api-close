package main

import (
	"context"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/openai"
)

// Bench mode wires the pipeline with fixed collaborators so k6 can measure
// gateway overhead against the mock upstream before identity, catalog and
// billing are connected. It must never be enabled in production.

type benchAuth struct{ key string }

func (a benchAuth) Authorize(_ context.Context, key string) (gateway.Principal, error) {
	if key != a.key {
		return gateway.Principal{}, gateway.ErrInvalidKey
	}
	return gateway.Principal{UserID: 1, KeyID: 1, Group: "default"}, nil
}

type benchPlanner struct{ target gateway.Target }

func (p benchPlanner) Plan(context.Context, *gateway.Request) ([]gateway.Target, error) {
	return []gateway.Target{p.target}, nil
}

func (benchPlanner) Report(gateway.Target, gateway.AttemptResult) {}

type benchSettler struct{}

func (benchSettler) Reserve(context.Context, *gateway.Request) error { return nil }

func (benchSettler) Finalize(context.Context, *gateway.Request, gateway.Outcome) error { return nil }

func benchDeps(upstream, key string) gateway.Deps {
	return gateway.Deps{
		Authorizer: benchAuth{key: key},
		Planner: benchPlanner{target: gateway.Target{
			ChannelID: 1, CredentialID: 1, Provider: openai.ID, BaseURL: upstream, Secret: "bench",
		}},
		Settler:   benchSettler{},
		Providers: map[string]gateway.Provider{openai.ID: openai.Provider{}},
	}
}
