package vertex

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/gemini"
)

func (p *Provider) Content(ctx context.Context, target gateway.Target, task native.Task) (*http.Response, error) {
	ctx = native.WithTarget(ctx, target)
	result, err := gemini.ParseResult(task.Data, task.UpstreamID)
	if err != nil || result.Status != "completed" || result.URL == "" {
		result, err = p.Poll(ctx, target, task)
	}
	if err != nil {
		return nil, err
	}
	if result.Status != "completed" || result.URL == "" {
		return nil, native.ErrContentUnsupported
	}
	if strings.HasPrefix(result.URL, "data:") {
		return gemini.InlineContent(result.URL)
	}
	u, err := url.Parse(result.URL)
	if err != nil || u.Host == "" || u.User != nil {
		return nil, errors.New("invalid Vertex video URI")
	}
	if u.Scheme == "gs" {
		if u.Path == "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("invalid Vertex storage URI")
		}
		u = &url.URL{Scheme: "https", Host: "storage.googleapis.com", Path: "/" + u.Host + u.Path}
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errors.New("unsupported Vertex video URI")
	}
	headers := make(http.Header)
	if u.Host == "storage.googleapis.com" && u.Scheme == "https" {
		auth, err := p.request(ctx, target, task.Model, "predictLongRunning", nil)
		if err != nil {
			return nil, err
		}
		_ = auth.Body.Close()
		if bearer := auth.Header.Get("Authorization"); bearer != "" {
			headers.Set("Authorization", bearer)
		}
		if project := auth.Header.Get("X-Goog-User-Project"); project != "" {
			headers.Set("X-Goog-User-Project", project)
		}
		// GCS is an explicitly trusted Google service, rather than an arbitrary
		// provider-returned host; authenticated storage GETs retain the account
		// transport and headers just like Vertex API requests.
		target.BaseURL = "https://storage.googleapis.com"
	}
	resp, err := native.Media(ctx, p.client, target, u.String(), headers)
	if err != nil {
		return nil, errors.New("vertex content transport failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, &native.Rejected{Status: resp.StatusCode}
	}
	return resp, nil
}
