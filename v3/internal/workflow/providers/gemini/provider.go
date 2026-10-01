// Package gemini implements Gemini Veo long-running video operations.
package gemini

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type Provider struct{ client *http.Client }

func New(client *http.Client) *Provider { return &Provider{native.Client(client)} }

var _ native.Adapter = (*Provider)(nil)

func (p *Provider) Submit(ctx context.Context, target gateway.Target, input native.Submit) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	if target.Secret == "" || strings.ContainsAny(target.Secret, "\r\n") {
		return native.Result{}, &native.InvalidRequest{Err: errors.New("invalid Gemini credential")}
	}
	body, err := Payload(input)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	model := input.Model
	if target.UpstreamModel != "" {
		model = target.UpstreamModel
	}
	model = strings.TrimPrefix(model, "models/")
	if !validSegment(model) {
		return native.Result{}, &native.InvalidRequest{Err: errors.New("invalid Veo model")}
	}
	endpoint, err := endpoint(target, "models/"+model+":predictLongRunning")
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	req, err := native.Request(ctx, http.MethodPost, endpoint, "", body)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	req.Header.Set("X-Goog-Api-Key", target.Secret)
	req.Header.Set("Accept", "application/json")
	data, err := native.JSON(p.client, req)
	if err != nil {
		return native.Result{}, err
	}
	result, err := ParseResult(data, "")
	if result.Status == "in_progress" {
		result.Status = "queued"
	}
	return result, err
}

func (p *Provider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	if !ValidOperation(task.UpstreamID) {
		return native.Result{}, errors.New("invalid Gemini operation name")
	}
	endpoint, err := endpoint(target, task.UpstreamID)
	if err != nil {
		return native.Result{}, err
	}
	req, err := native.Request(ctx, http.MethodGet, endpoint, "", nil)
	if err != nil {
		return native.Result{}, err
	}
	req.Header.Set("X-Goog-Api-Key", target.Secret)
	req.Header.Set("Accept", "application/json")
	data, err := native.JSON(p.client, req)
	if err != nil {
		return native.Result{}, err
	}
	return ParseResult(data, task.UpstreamID)
}

func (p *Provider) Content(ctx context.Context, target gateway.Target, task native.Task) (*http.Response, error) {
	ctx = native.WithTarget(ctx, target)
	result, err := ParseResult(task.Data, task.UpstreamID)
	if err != nil || result.Status != "completed" || result.URL == "" {
		result, err = p.Poll(ctx, target, task)
	}
	if err != nil {
		return nil, err
	}
	if result.Status != "completed" || result.URL == "" {
		return nil, native.ErrContentUnsupported
	}
	return Content(ctx, p.client, target, result.URL)
}

func endpoint(target gateway.Target, path string) (string, error) {
	base := strings.TrimRight(target.BaseURL, "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	if !strings.HasSuffix(base, "/v1") && !strings.HasSuffix(base, "/v1beta") {
		base += "/v1beta"
	}
	target.BaseURL = base
	return native.Endpoint(target, "", path)
}

// ValidOperation validates Google's slash-bearing resource name without escaping
// its separators or allowing traversal/query injection.
func ValidOperation(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) < 2 || parts[len(parts)-2] != "operations" {
		return false
	}
	for _, part := range parts {
		if !validSegment(part) {
			return false
		}
	}
	return true
}

func validSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '@'
		if !valid {
			return false
		}
	}
	return true
}
