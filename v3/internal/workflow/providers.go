package workflow

import (
	"context"
	"errors"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/ali"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/doubao"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/gemini"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/hailuo"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/jimeng"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/kling"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/openai_video"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/suno"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/vertex"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/vidu"
)

// DefaultProviders uses the same channel provider IDs as catalog migration.
func DefaultProviders(client *http.Client) map[string]native.Adapter {
	openAI, doubaoVideo, minimax := openai_video.New(client), doubao.New(client), hailuo.New(client)
	return map[string]native.Adapter{
		"openai": openAI, "openai_video": openAI, "suno": suno.New(client), "kling": kling.New(client),
		"jimeng": jimeng.New(client), "vidu": vidu.New(client), "doubao_video": doubaoVideo,
		"volcengine": doubaoVideo, "minimax": minimax, "hailuo": minimax, "ali": ali.New(client),
		"gemini": gemini.New(client), "vertex": vertex.New(client),
	}
}

// SnapshotResolver restores a target by immutable IDs. Install a DB-backed
// resolver instead when tasks must continue after their channel is disabled;
// that lookup belongs to the background worker, outside the gateway hot path.
func SnapshotResolver(snapshot func() *catalog.Snapshot) TargetResolver {
	return func(_ context.Context, channelID, credentialID int64) (gateway.Target, error) {
		snap := snapshot()
		if snap == nil {
			return gateway.Target{}, errors.New("task catalog unavailable")
		}
		channel := snap.Channels[channelID]
		if channel == nil {
			return gateway.Target{}, errors.New("task channel unavailable")
		}
		for _, credential := range channel.Credentials {
			if credential.ID == credentialID {
				return gateway.Target{ChannelID: channel.ID, CredentialID: credential.ID, Provider: channel.Provider,
					BaseURL: channel.BaseURL, Secret: credential.Secret, ProxyURL: channel.ProxyURL,
					Scope: channel.Scope, OwnerUserID: channel.OwnerUserID, Settings: channel.Settings,
					ParamOverride: channel.ParamOverride, HeaderOverride: channel.HeaderOverride,
					StatusCodeMapping: channel.StatusCodeMapping, Fingerprint: gateway.CredentialFingerprint{
						UserAgent: credential.Fingerprint.UserAgent, TLSProfile: credential.Fingerprint.TLSProfile}}, nil
			}
		}
		return gateway.Target{}, errors.New("task credential unavailable")
	}
}
