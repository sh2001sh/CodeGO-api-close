// Package ali implements DashScope asynchronous video generation.
package ali

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
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
		return native.Result{}, &native.InvalidRequest{Err: errors.New("invalid Ali credential")}
	}
	body, err := payload(input, target.UpstreamModel)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	endpoint, err := native.Endpoint(target, "https://dashscope.aliyuncs.com", "/api/v1/services/aigc/video-generation/video-synthesis")
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	req, err := native.Request(ctx, http.MethodPost, endpoint, target.Secret, body)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	req.Header.Set("X-DashScope-Async", "enable")
	data, err := native.JSON(p.client, req)
	if err != nil {
		return native.Result{}, err
	}
	return parse(data, "")
}

func (p *Provider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	if task.UpstreamID == "" || task.UpstreamID == "." || task.UpstreamID == ".." || strings.ContainsAny(task.UpstreamID, "/\\?#") {
		return native.Result{}, errors.New("missing Ali task ID")
	}
	endpoint, err := native.Endpoint(target, "https://dashscope.aliyuncs.com", "/api/v1/tasks/"+url.PathEscape(task.UpstreamID))
	if err != nil {
		return native.Result{}, err
	}
	req, err := native.Request(ctx, http.MethodGet, endpoint, target.Secret, nil)
	if err != nil {
		return native.Result{}, err
	}
	data, err := native.JSON(p.client, req)
	if err != nil {
		return native.Result{}, err
	}
	return parse(data, task.UpstreamID)
}

func (p *Provider) Content(ctx context.Context, target gateway.Target, task native.Task) (*http.Response, error) {
	ctx = native.WithTarget(ctx, target)
	result, err := parse(task.Data, task.UpstreamID)
	if err != nil || result.Status != "completed" || result.URL == "" {
		result, err = p.Poll(ctx, target, task)
	}
	if err != nil {
		return nil, err
	}
	if result.Status != "completed" || result.URL == "" {
		return nil, native.ErrContentUnsupported
	}
	// DashScope video links are signed public media links; never forward the API key.
	if target.BaseURL == "" {
		target.BaseURL = "https://dashscope.aliyuncs.com"
	}
	resp, err := native.Media(ctx, p.client, target, result.URL, nil)
	if err != nil {
		return nil, errors.New("ali content transport failed")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, &native.Rejected{Status: resp.StatusCode}
	}
	return resp, nil
}

func parse(data []byte, fallback string) (native.Result, error) {
	var response struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Output  struct {
			ID      string `json:"task_id"`
			Status  string `json:"task_status"`
			URL     string `json:"video_url"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"output"`
		Usage struct {
			Duration json.RawMessage `json:"duration"`
			Count    json.RawMessage `json:"video_count"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &response) != nil {
		return native.Result{}, errors.New("invalid Ali task response")
	}
	r := native.Result{ID: response.Output.ID, Status: native.Status(response.Output.Status), URL: response.Output.URL, Data: append(json.RawMessage(nil), data...)}
	if r.ID == "" {
		r.ID = fallback
	}
	if response.Code != "" || response.Output.Code != "" || strings.EqualFold(response.Output.Status, "UNKNOWN") {
		r.Status = "failed"
	}
	if r.Status == "failed" {
		r.Error = response.Output.Message
		if r.Error == "" {
			r.Error = response.Message
		}
		if r.Error == "" {
			r.Error = "Ali video generation failed"
		}
	} else if r.ID == "" {
		return native.Result{}, errors.New("ali task ID missing")
	}
	if r.Status == "completed" {
		duration, count := number(response.Usage.Duration), number(response.Usage.Count)
		if count == 0 {
			count = 1
		}
		if duration > 0 && count > 0 {
			r.Units = duration * count
		}
	}
	return r, nil
}

func number(raw json.RawMessage) float64 {
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			n, _ = strconv.ParseFloat(s, 64)
		}
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return 0
	}
	return n
}
