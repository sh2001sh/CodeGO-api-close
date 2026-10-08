package live

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/azure"
	"github.com/sh2001sh/new-api/v3/internal/gateway/providers/responses"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (h *Handler) submitBackground(ctx context.Context, job *BackgroundJob, req *gateway.Request, target gateway.Target) error {
	wire, provider, upstream, err, done := h.buildBackgroundSubmitRequest(ctx, job, req, target)
	if done {
		return err
	}
	resp, err, done := h.sendBackgroundSubmitRequest(ctx, job, provider, wire, upstream, target)
	if done {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return h.handleBackgroundSubmitResponse(ctx, job, req, target, provider, wire, resp)
}

// buildBackgroundSubmitRequest prepares the outbound wire request (stream/background flags,
// file references) and builds the upstream HTTP request for job's provider and target. If
// provider resolution or request construction fails, it marks the job failed in place and
// returns done=true so the caller returns immediately (with a nil error, matching original
// behavior: these are terminal job states, not transport errors).
func (h *Handler) buildBackgroundSubmitRequest(ctx context.Context, job *BackgroundJob, req *gateway.Request, target gateway.Target) (wire *gateway.Request, provider gateway.Provider, upstream *http.Request, err error, done bool) {
	wire = &gateway.Request{}
	*wire = *req
	wire.Body = bytes.Clone(req.Body)
	wire.Body, _ = sjson.SetBytes(wire.Body, "stream", true)
	if job.Native {
		wire.Body, _ = sjson.SetBytes(wire.Body, "background", true)
		provider = responses.Provider{}
		if target.Provider == "azure" {
			provider = azure.Provider{}
		}
	} else {
		wire.Body, _ = sjson.DeleteBytes(wire.Body, "background")
		provider = h.cfg.Providers[target.Provider]
	}
	if provider == nil {
		job.Status, job.Error = "failed", "provider_not_supported"
		return wire, provider, nil, nil, true
	}
	preparedBody, err := h.PrepareFileReferences(ctx, wire, target)
	if err != nil {
		job.Status, job.Error = "failed", "background_file_reference_failed"
		return wire, provider, nil, nil, true
	}
	wire.Body = preparedBody
	upstream, err = gateway.BuildProviderRequest(ctx, provider, wire, target)
	if err != nil {
		job.Status, job.Error = "failed", "background_request_invalid"
		return wire, provider, nil, nil, true
	}
	if err := gateway.ApplyUpstreamRequest(upstream, wire, target); err != nil {
		_ = upstream.Body.Close()
		job.Status, job.Error = "failed", "background_request_override_invalid"
		return wire, provider, nil, nil, true
	}
	if err := gateway.FinalizeProviderRequest(ctx, provider, upstream, wire, target); err != nil {
		_ = upstream.Body.Close()
		job.Status, job.Error = "failed", "background_request_finalize_failed"
		return wire, provider, nil, nil, true
	}
	return wire, provider, upstream, nil, false
}

// sendBackgroundSubmitRequest acquires an upstream client, durably marks the job as submitting,
// and sends the upstream request. done=true tells the caller to return err immediately: either a
// transport-level error, or the outcome of markBackgroundUnknown when the send's own success is
// unknown and must be reconciled on the next lease.
func (h *Handler) sendBackgroundSubmitRequest(ctx context.Context, job *BackgroundJob, provider gateway.Provider, wire *gateway.Request, upstream *http.Request, target gateway.Target) (resp *http.Response, err error, done bool) {
	client, err := h.upstreamClient(ctx, target)
	if err != nil {
		_ = upstream.Body.Close()
		return nil, err, true
	}
	client.Transport = gateway.OverrideProviderTransport(provider, wire, client.Transport)
	job.Status = "submitting"
	job.AttemptsCount++
	if err := h.cfg.BackgroundJobs.Save(ctx, *job); err != nil {
		_ = upstream.Body.Close()
		return nil, err, true
	}
	resp, err = client.Do(upstream)
	if err != nil {
		return nil, h.markBackgroundUnknown(context.WithoutCancel(ctx), job), true
	}
	return resp, nil, false
}

// handleBackgroundSubmitResponse maps the upstream status and dispatches to native polling or
// stream draining on success. On a provider rejection of the background flag it falls the job
// back to non-native and resubmits; on explicit 4xx it fails the job in place (never resubmits
// an accepted request); anything else goes through markBackgroundUnknown for reconciliation.
func (h *Handler) handleBackgroundSubmitResponse(ctx context.Context, job *BackgroundJob, req *gateway.Request, target gateway.Target, provider gateway.Provider, wire *gateway.Request, resp *http.Response) error {
	resp.StatusCode = gateway.MapUpstreamStatus(resp.StatusCode, target)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if job.Native && (resp.StatusCode == 400 || resp.StatusCode == 422) && backgroundUnsupported(body) {
			job.Native, job.Status = false, "queued"
			if err := h.cfg.BackgroundJobs.Save(ctx, *job); err != nil {
				return err
			}
			return h.submitBackground(ctx, job, req, target)
		}
		// Only explicit 4xx rejection proves no acceptance. Server errors and
		// redirects can originate after an accepted submission; never resubmit.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			job.Status, job.Error = "failed", "background_upstream_rejected"
			return nil
		}
		return h.markBackgroundUnknown(ctx, job)
	}
	if job.Native {
		return h.consumeNativeBackground(ctx, job, req, resp)
	}
	return h.drainBackgroundSubmitStream(ctx, job, req, wire, provider, resp)
}

// drainBackgroundSubmitStream reads a non-native submission's decoded event stream to
// completion, persisting each event and tracking usage/delivery, until a terminal event or
// stream error ends the attempt.
func (h *Handler) drainBackgroundSubmitStream(ctx context.Context, job *BackgroundJob, req *gateway.Request, wire *gateway.Request, provider gateway.Provider, resp *http.Response) error {
	stream := provider.Decode(wire, resp)
	defer func() { _ = stream.Close() }()
	for {
		event, err := stream.Next()
		if err != nil {
			if job.UpstreamID == "" && !job.Delivered {
				return h.markBackgroundUnknown(context.WithoutCancel(ctx), job)
			}
			return err
		}
		if event.ServiceTier != "" {
			job.Usage.ServiceTier = event.ServiceTier
		}
		if event.Usage != nil {
			backgroundApplyUsage(job, *event.Usage)
		}
		if event.Kind == gateway.EventDone {
			if !backgroundTerminal(job.Status) {
				job.Status = "completed"
			}
			return nil
		}
		job.GeneratedBytes += int64(event.TextBytes)
		if event.Kind == gateway.EventData {
			job.Delivered = true
		}
		if len(event.Payload) > 0 {
			if err := h.persistBackgroundEvent(ctx, job, req, event.Name, event.Payload); err != nil {
				return err
			}
		}
		if event.Kind == gateway.EventError {
			job.Status, job.Error = "failed", "background_upstream_error"
			return nil
		}
		if err := h.cfg.BackgroundJobs.Save(ctx, *job); err != nil {
			return err
		}
	}
}

func backgroundUnsupported(body []byte) bool {
	root := gjson.ParseBytes(body)
	message := strings.ToLower(root.Get("error.message").Str)
	return root.Get("error.param").Str == "background" || (strings.Contains(message, "background") && (strings.Contains(message, "unsupported") || strings.Contains(message, "not supported") || strings.Contains(message, "unknown")))
}
