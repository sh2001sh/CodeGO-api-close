// Package vertex implements Vertex AI Veo predictLongRunning operations.
package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	vertexgateway "github.com/sh2001sh/new-api/v3/internal/gateway/providers/vertex"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/gemini"
)

type Provider struct {
	client *http.Client
	auth   vertexgateway.Provider
}

func New(client *http.Client) *Provider {
	bounded := native.Client(client)
	return &Provider{client: bounded, auth: vertexgateway.Provider{HTTPClient: bounded}}
}

var _ native.Adapter = (*Provider)(nil)

func (p *Provider) Submit(ctx context.Context, target gateway.Target, input native.Submit) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	body, err := gemini.Payload(input)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	model := input.Model
	if target.UpstreamModel != "" {
		model = target.UpstreamModel
	}
	req, err := p.request(ctx, target, model, "predictLongRunning", body)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	data, err := native.JSON(p.client, req)
	if err != nil {
		return native.Result{}, err
	}
	result, err := gemini.ParseResult(data, "")
	if result.Status == "in_progress" {
		result.Status = "queued"
	}
	return result, err
}

func (p *Provider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	if !gemini.ValidOperation(task.UpstreamID) {
		return native.Result{}, errors.New("invalid Vertex operation name")
	}
	parts := strings.Split(task.UpstreamID, "/")
	model, region, resource := "", "", ""
	if len(parts) == 10 && parts[0] == "projects" && parts[2] == "locations" && parts[4] == "publishers" && parts[5] == "google" && parts[6] == "models" {
		model, region, resource = parts[7], parts[3], strings.Join(parts[:8], "/")
	} else if len(parts) == 6 && parts[0] == "publishers" && parts[1] == "google" && parts[2] == "models" {
		model, resource = parts[3], strings.Join(parts[:4], "/")
	} else {
		return native.Result{}, errors.New("invalid Vertex operation resource")
	}
	body, _ := json.Marshal(map[string]string{"operationName": task.UpstreamID})
	target.UpstreamModel = model
	req, err := p.request(ctx, target, model, "fetchPredictOperation", body)
	if err != nil {
		return native.Result{}, err
	}
	// The persisted operation contains the original project/region/model even if
	// the channel's region settings have subsequently changed.
	versionEnd := strings.Index(req.URL.Path, "/projects/")
	if versionEnd < 0 {
		versionEnd = strings.Index(req.URL.Path, "/publishers/google/models/")
	}
	if versionEnd < 0 {
		return native.Result{}, errors.New("invalid Vertex operation endpoint")
	}
	req.URL.Path = req.URL.Path[:versionEnd] + "/" + resource + ":fetchPredictOperation"
	if target.BaseURL == "" && region != "" {
		req.URL.Host = "aiplatform.googleapis.com"
		if region != "global" {
			req.URL.Host = region + "-aiplatform.googleapis.com"
		}
	}
	data, err := native.JSON(p.client, req)
	if err != nil {
		return native.Result{}, err
	}
	return gemini.ParseResult(data, task.UpstreamID)
}

func (p *Provider) request(ctx context.Context, target gateway.Target, model, action string, body []byte) (*http.Request, error) {
	// BuildRequest owns credential parsing, JWT/OAuth caching, regional endpoints
	// and express API-key authentication. Only the Veo action/body differ.
	req, err := p.auth.BuildRequest(ctx, &gateway.Request{Model: model, Protocol: gateway.ProtocolGemini, Body: body}, target)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(req.URL.Path, ":generateContent") {
		_ = req.Body.Close()
		return nil, errors.New("vertex Veo requires a Google model")
	}
	req.URL.Path = strings.TrimSuffix(req.URL.Path, ":generateContent") + ":" + action
	req.Header.Set("Accept", "application/json")
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	return req, nil
}
