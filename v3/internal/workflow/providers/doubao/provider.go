package doubao

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type Provider struct{ client *http.Client }

func New(client *http.Client) *Provider { return &Provider{native.Client(client)} }

var _ native.Adapter = (*Provider)(nil)

func (p *Provider) Submit(ctx context.Context, target gateway.Target, input native.Submit) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	body, err := requestBody(input.Body, target.UpstreamModel)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	return p.request(ctx, target, http.MethodPost, "/api/v3/contents/generations/tasks", body, "")
}

func (p *Provider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	if task.UpstreamID == "" {
		return native.Result{}, &native.InvalidRequest{Err: errors.New("missing upstream task ID")}
	}
	return p.request(ctx, target, http.MethodGet, "/api/v3/contents/generations/tasks/"+url.PathEscape(task.UpstreamID), nil, task.UpstreamID)
}

func (p *Provider) Content(context.Context, gateway.Target, native.Task) (*http.Response, error) {
	return nil, native.ErrContentUnsupported
}

func (p *Provider) request(ctx context.Context, target gateway.Target, method, path string, body []byte, id string) (native.Result, error) {
	endpoint, err := native.Endpoint(target, "https://ark.cn-beijing.volces.com", path)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	req, err := native.Request(ctx, method, endpoint, target.Secret, body)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	req.Header.Set("Accept", "application/json")
	data, err := native.JSON(p.client, req)
	if err != nil {
		return native.Result{}, err
	}
	return decodeResponse(data, id)
}

type doubaoResponse struct {
	ID       string  `json:"id"`
	Status   string  `json:"status"`
	Duration float64 `json:"duration"`
	Content  struct {
		URL string `json:"video_url"`
	} `json:"content"`
	Usage struct {
		Completion int64 `json:"completion_tokens"`
		Total      int64 `json:"total_tokens"`
		ToolUsage  struct {
			WebSearch int64 `json:"web_search"`
		} `json:"tool_usage"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// decodeResponse parses the doubao task response, validates its ID/status,
// and builds the resulting native.Result, including usage for completed
// tasks.
func decodeResponse(data []byte, id string) (native.Result, error) {
	var response doubaoResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return native.Result{}, errors.New("invalid doubao task response")
	}
	if response.ID == "" {
		response.ID = id
	}
	if response.ID == "" {
		if response.Error != nil && response.Error.Message != "" {
			return native.Result{}, &native.Rejected{Status: http.StatusBadRequest}
		}
		return native.Result{}, errors.New("doubao task ID missing")
	}
	if response.Status == "" && id != "" {
		return native.Result{}, errors.New("doubao task status missing")
	}
	switch response.Status {
	case "", "pending", "queued", "processing", "running", "succeeded", "failed", "cancelled", "canceled":
	default:
		return native.Result{}, errors.New("unknown doubao task status")
	}
	r := native.Result{ID: response.ID, Status: native.Status(response.Status), Data: data, URL: response.Content.URL}
	if response.Error != nil {
		r.Error = response.Error.Message
		r.Status = "failed"
	}
	if r.Status == "completed" {
		if response.Duration < 0 || response.Usage.Completion < 0 || response.Usage.Total < 0 || response.Usage.ToolUsage.WebSearch < 0 {
			return native.Result{}, errors.New("invalid doubao task usage")
		}
		r.Units = response.Duration
		r.Usage.CompletionTokens = response.Usage.Completion
		if response.Usage.Total > response.Usage.Completion {
			r.Usage.PromptTokens = response.Usage.Total - response.Usage.Completion
		}
		if response.Usage.ToolUsage.WebSearch > 0 {
			r.Usage.ToolCalls = map[string]int64{"web_search": response.Usage.ToolUsage.WebSearch}
		}
	}
	return r, nil
}
