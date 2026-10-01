package live

import (
	"context"
	"errors"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (h *Handler) previousResponse(ctx context.Context, req *gateway.Request, id string) (string, int64, int64, error) {
	if strings.HasPrefix(id, "resp_bg_") {
		if h.cfg.BackgroundJobs == nil {
			return "", 0, 0, ErrNotFound
		}
		job, err := h.cfg.BackgroundJobs.GetOwned(ctx, id, req.Principal.UserID, req.Principal.KeyID)
		if err != nil {
			return "", 0, 0, err
		}
		if job.UserID != req.Principal.UserID || job.KeyID != req.Principal.KeyID || job.Status != "completed" || job.UpstreamID == "" {
			return "", 0, 0, ErrNotFound
		}
		return job.UpstreamID, job.ChannelID, job.CredentialID, nil
	}
	locator, err := h.cfg.Repository.Get(ctx, id, req.Principal.UserID, req.Principal.KeyID)
	if err != nil {
		return "", 0, 0, err
	}
	if locator.UserID != req.Principal.UserID || locator.KeyID != req.Principal.KeyID {
		return "", 0, 0, ErrNotFound
	}
	return id, locator.ChannelID, locator.CredentialID, nil
}

// Plan wraps the base route plan with owner-scoped Responses continuation pins.
// Wire the Handler itself as the core gateway Planner after constructing live
// with the original planner. Report delegates to that original planner.
func (h *Handler) Plan(ctx context.Context, req *gateway.Request) ([]gateway.Target, error) {
	targets, err := h.cfg.Planner.Plan(ctx, req)
	if err != nil {
		return nil, err
	}
	if req.Protocol != gateway.ProtocolResponses {
		return targets, nil
	}
	id := gjson.GetBytes(req.Body, "previous_response_id").Str
	if id == "" {
		return targets, nil
	}
	_, channel, credential, err := h.previousResponse(ctx, req, id)
	if err != nil {
		return nil, err
	}
	filtered := targets[:0]
	for _, target := range targets {
		if target.ChannelID == channel && target.CredentialID == credential {
			filtered = append(filtered, target)
		}
	}
	if len(filtered) == 0 {
		return nil, errors.New("live: previous response route is no longer permitted")
	}
	return filtered, nil
}

func (h *Handler) Report(target gateway.Target, result gateway.AttemptResult) {
	h.cfg.Planner.Report(target, result)
}

func (h *Handler) preparePreviousResponse(ctx context.Context, req *gateway.Request, target gateway.Target, body []byte) ([]byte, error) {
	if req.Protocol != gateway.ProtocolResponses {
		return body, nil
	}
	id := gjson.GetBytes(body, "previous_response_id").Str
	if id == "" {
		return body, nil
	}
	upstream, channel, credential, err := h.previousResponse(ctx, req, id)
	if err != nil {
		return nil, err
	}
	if target.ChannelID != channel || target.CredentialID != credential {
		return nil, errors.New("live: response continuation must use its original credential")
	}
	return sjson.SetBytes(body, "previous_response_id", upstream)
}
