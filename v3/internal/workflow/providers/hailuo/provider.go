package hailuo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type Provider struct{ client *http.Client }

func New(client *http.Client) *Provider { return &Provider{native.Client(client)} }

var _ native.Adapter = (*Provider)(nil)

type response struct {
	ID       string  `json:"task_id"`
	Status   string  `json:"status"`
	FileID   string  `json:"file_id"`
	Duration float64 `json:"duration"`
	Base     struct {
		Code    int    `json:"status_code"`
		Message string `json:"status_msg"`
	} `json:"base_resp"`
}

func (p *Provider) Submit(ctx context.Context, target gateway.Target, input native.Submit) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	body, err := requestBody(input.Body, target.UpstreamModel)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	data, err := p.request(ctx, target, http.MethodPost, "/v1/video_generation", body)
	if err != nil {
		return native.Result{}, err
	}
	return parse(data, "")
}

func (p *Provider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	if task.UpstreamID == "" {
		return native.Result{}, &native.InvalidRequest{Err: errors.New("missing upstream task ID")}
	}
	data, err := p.request(ctx, target, http.MethodGet, "/v1/query/video_generation?task_id="+url.QueryEscape(task.UpstreamID), nil)
	if err != nil {
		return native.Result{}, err
	}
	r, err := parse(data, task.UpstreamID)
	if err != nil || r.Status != "completed" {
		return r, err
	}
	var taskResponse response
	if json.Unmarshal(data, &taskResponse) != nil || taskResponse.FileID == "" {
		return native.Result{}, errors.New("hailuo completed task missing file ID")
	}
	fileData, err := p.request(ctx, target, http.MethodGet, "/v1/files/retrieve?file_id="+url.QueryEscape(taskResponse.FileID), nil)
	if err != nil {
		return native.Result{}, err
	}
	var file struct {
		File struct {
			URL string `json:"download_url"`
		} `json:"file"`
		Base struct {
			Code int `json:"status_code"`
		} `json:"base_resp"`
	}
	if json.Unmarshal(fileData, &file) != nil || file.Base.Code != 0 || file.File.URL == "" {
		return native.Result{}, errors.New("hailuo file retrieval failed")
	}
	r.URL = file.File.URL
	return r, nil
}

func (p *Provider) Content(context.Context, gateway.Target, native.Task) (*http.Response, error) {
	return nil, native.ErrContentUnsupported
}

func (p *Provider) request(ctx context.Context, target gateway.Target, method, path string, body []byte) ([]byte, error) {
	if strings.HasSuffix(strings.TrimRight(target.BaseURL, "/"), "/v1") {
		path = strings.TrimPrefix(path, "/v1")
	}
	endpoint, err := native.Endpoint(target, "https://api.minimax.io", path)
	if err != nil {
		return nil, &native.InvalidRequest{Err: err}
	}
	req, err := native.Request(ctx, method, endpoint, target.Secret, body)
	if err != nil {
		return nil, &native.InvalidRequest{Err: err}
	}
	req.Header.Set("Accept", "application/json")
	return native.JSON(p.client, req)
}

func parse(data []byte, fallbackID string) (native.Result, error) {
	var response response
	if json.Unmarshal(data, &response) != nil {
		return native.Result{}, errors.New("invalid hailuo response")
	}
	failed := strings.EqualFold(response.Status, "fail") || strings.EqualFold(response.Status, "failed")
	if response.Base.Code != 0 && (fallbackID == "" || !failed) {
		return native.Result{}, &native.Rejected{Status: http.StatusBadRequest}
	}
	if response.ID == "" {
		response.ID = fallbackID
	}
	if response.ID == "" {
		return native.Result{}, errors.New("hailuo task ID missing")
	}
	if response.Status == "" && fallbackID != "" {
		return native.Result{}, errors.New("hailuo task status missing")
	}
	status := "queued"
	switch strings.ToLower(response.Status) {
	case "", "preparing", "queueing":
	case "processing":
		status = "in_progress"
	case "success":
		status = "completed"
	case "fail", "failed":
		status = "failed"
	default:
		return native.Result{}, errors.New("unknown hailuo task state")
	}
	r := native.Result{ID: response.ID, Status: status, Data: data}
	if status == "completed" {
		if response.Duration < 0 {
			return native.Result{}, errors.New("invalid hailuo duration")
		}
		r.Units = response.Duration
	}
	if status == "failed" {
		r.Error = response.Base.Message
		if r.Error == "" {
			r.Error = "task failed"
		}
	}
	return r, nil
}
