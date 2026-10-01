package vidu

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
	body, action, err := requestBody(input, target.UpstreamModel)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	path := "/text2video"
	switch action {
	case "generate", "img2video":
		path = "/img2video"
	case "firstTailGenerate", "start-end2video":
		path = "/start-end2video"
	case "referenceGenerate", "reference2video":
		path = "/reference2video"
	}
	return p.request(ctx, target, http.MethodPost, "/ent/v2"+path, body, "")
}

func (p *Provider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	if task.UpstreamID == "" {
		return native.Result{}, &native.InvalidRequest{Err: errors.New("missing upstream task ID")}
	}
	return p.request(ctx, target, http.MethodGet, "/ent/v2/tasks/"+url.PathEscape(task.UpstreamID)+"/creations", nil, task.UpstreamID)
}

func (p *Provider) Content(context.Context, gateway.Target, native.Task) (*http.Response, error) {
	return nil, native.ErrContentUnsupported
}

func (p *Provider) request(ctx context.Context, target gateway.Target, method, path string, body []byte, id string) (native.Result, error) {
	endpoint, err := native.Endpoint(target, "https://api.vidu.com", path)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	req, err := native.Request(ctx, method, endpoint, "", body)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	req.Header.Set("Authorization", "Token "+target.Secret)
	req.Header.Set("Accept", "application/json")
	data, err := native.JSON(p.client, req)
	if err != nil {
		return native.Result{}, err
	}
	return decodeResponse(data, id)
}

type viduResponse struct {
	ID        string  `json:"task_id"`
	State     string  `json:"state"`
	Error     string  `json:"err_code"`
	Duration  float64 `json:"duration"`
	Creations []struct {
		URL      string  `json:"url"`
		Duration float64 `json:"duration"`
	} `json:"creations"`
}

// decodeResponse parses the vidu task response, validates its ID/state, and
// builds the resulting native.Result, including billed units for completed
// tasks.
func decodeResponse(data []byte, id string) (native.Result, error) {
	var response viduResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return native.Result{}, errors.New("invalid vidu response")
	}
	if response.ID == "" {
		response.ID = id
	}
	if response.ID == "" {
		if response.State == "failed" || response.Error != "" {
			return native.Result{}, &native.Rejected{Status: http.StatusBadRequest}
		}
		return native.Result{}, errors.New("vidu task ID missing")
	}
	if response.State == "" && id != "" {
		return native.Result{}, errors.New("vidu task state missing")
	}
	status := "queued"
	switch response.State {
	case "created", "queueing", "queued", "":
	case "processing":
		status = "in_progress"
	case "success":
		status = "completed"
	case "failed":
		status = "failed"
	default:
		return native.Result{}, errors.New("unknown vidu task state")
	}
	r := native.Result{ID: response.ID, Status: status, Error: response.Error, Data: data}
	if status == "completed" {
		if response.Duration < 0 {
			return native.Result{}, errors.New("invalid vidu duration")
		}
		r.Units = response.Duration
		if len(response.Creations) > 0 {
			r.URL = response.Creations[0].URL
			if response.Creations[0].Duration < 0 {
				return native.Result{}, errors.New("invalid vidu creation duration")
			}
			if response.Creations[0].Duration > 0 {
				r.Units = response.Creations[0].Duration
			}
		}
	}
	return r, nil
}
