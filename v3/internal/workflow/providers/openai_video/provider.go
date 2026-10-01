package openai_video

import (
	"context"
	"encoding/json"
	"errors"
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
	path := "/v1/videos"
	if input.Action == "remix" || input.Action == "remixGenerate" {
		if input.OriginID == "" {
			return native.Result{}, &native.InvalidRequest{Err: errors.New("remix requires upstream origin ID")}
		}
		path += "/" + url.PathEscape(input.OriginID) + "/remix"
	}
	body, contentType, err := requestBody(input, target.UpstreamModel)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	endpoint, err := endpoint(target, path)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	req, err := native.Request(ctx, http.MethodPost, endpoint, target.Secret, body)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	req.Header.Set("Content-Type", contentType)
	data, err := native.JSON(p.client, req)
	if err != nil {
		return native.Result{}, err
	}
	return parse(data, "")
}

func (p *Provider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	req, err := taskRequest(ctx, target, task, "")
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
	req, err := taskRequest(ctx, target, task, "/content")
	if err != nil {
		return nil, err
	}
	resp, err := native.Do(p.client, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, &native.Rejected{Status: resp.StatusCode}
	}
	return resp, nil
}

func taskRequest(ctx context.Context, target gateway.Target, task native.Task, suffix string) (*http.Request, error) {
	ctx = native.WithTarget(ctx, target)
	if task.UpstreamID == "" {
		return nil, &native.InvalidRequest{Err: errors.New("missing upstream task ID")}
	}
	endpoint, err := endpoint(target, "/v1/videos/"+url.PathEscape(task.UpstreamID)+suffix)
	if err != nil {
		return nil, &native.InvalidRequest{Err: err}
	}
	req, err := native.Request(ctx, http.MethodGet, endpoint, target.Secret, nil)
	if err != nil {
		return nil, &native.InvalidRequest{Err: err}
	}
	return req, nil
}

func endpoint(target gateway.Target, path string) (string, error) {
	if strings.HasSuffix(strings.TrimRight(target.BaseURL, "/"), "/v1") {
		path = strings.TrimPrefix(path, "/v1")
	}
	return native.Endpoint(target, "https://api.openai.com", path)
}

func parse(data []byte, fallbackID string) (native.Result, error) {
	var response struct {
		ID      string          `json:"id"`
		TaskID  string          `json:"task_id"`
		Status  string          `json:"status"`
		Seconds json.RawMessage `json:"seconds"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return native.Result{}, errors.New("invalid video task response")
	}
	if response.ID == "" {
		response.ID = response.TaskID
	}
	if response.ID == "" {
		response.ID = fallbackID
	}
	if response.ID == "" {
		if response.Error != nil && response.Error.Message != "" {
			return native.Result{}, &native.Rejected{Status: http.StatusBadRequest}
		}
		return native.Result{}, errors.New("video task ID missing")
	}
	switch response.Status {
	case "":
		if fallbackID != "" {
			return native.Result{}, errors.New("video task status missing")
		}
	case "queued", "pending", "processing", "in_progress", "completed", "failed", "cancelled", "canceled":
	default:
		return native.Result{}, errors.New("unknown video task status")
	}
	r := native.Result{ID: response.ID, Status: native.Status(response.Status), Data: append(json.RawMessage(nil), data...)}
	if response.Error != nil {
		r.Error = response.Error.Message
		r.Status = "failed"
	}
	if r.Status == "completed" {
		var seconds float64
		if json.Unmarshal(response.Seconds, &seconds) != nil {
			var value string
			if json.Unmarshal(response.Seconds, &value) == nil {
				seconds, _ = strconv.ParseFloat(value, 64)
			}
		}
		if seconds > 0 {
			r.Units = seconds
		}
	}
	return r, nil
}
