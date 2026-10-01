package kling

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
	body, action, err := requestBody(input, target.UpstreamModel)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	return p.request(ctx, target, http.MethodPost, action, "", body)
}

func (p *Provider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	if task.UpstreamID == "" {
		return native.Result{}, invalid("missing upstream task ID")
	}
	action := task.Action
	var saved struct {
		Action string `json:"_v3_action"`
	}
	if json.Unmarshal(task.Data, &saved) == nil && saved.Action != "" {
		action = saved.Action
	}
	return p.request(ctx, target, http.MethodGet, action, task.UpstreamID, nil)
}

func (p *Provider) Content(context.Context, gateway.Target, native.Task) (*http.Response, error) {
	return nil, native.ErrContentUnsupported
}

func (p *Provider) request(ctx context.Context, target gateway.Target, method, action, id string, body []byte) (native.Result, error) {
	data, err := p.send(ctx, target, method, action, id, body)
	if err != nil {
		return native.Result{}, err
	}
	return decodeResponse(data, method, action, id)
}

// send builds the authenticated upstream HTTP request for the given
// action/id and returns the raw response body.
func (p *Provider) send(ctx context.Context, target gateway.Target, method, action, id string, body []byte) ([]byte, error) {
	path := "/v1/videos/text2video"
	if action == "generate" || action == "image2video" || action == "firstTailGenerate" {
		path = "/v1/videos/image2video"
	}
	if strings.HasPrefix(target.Secret, "sk-") {
		path = "/kling" + path
	}
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	endpoint, err := native.Endpoint(target, "https://api.klingai.com", path)
	if err != nil {
		return nil, &native.InvalidRequest{Err: err}
	}
	token, err := authToken(target.Secret)
	if err != nil {
		return nil, &native.InvalidRequest{Err: err}
	}
	req, err := native.Request(ctx, method, endpoint, token, body)
	if err != nil {
		return nil, &native.InvalidRequest{Err: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "kling-sdk/1.0")
	return native.JSON(p.client, req)
}

type klingResponse struct {
	Code    *int   `json:"code"`
	Message string `json:"message"`
	TaskID  string `json:"task_id"`
	Data    struct {
		ID        string          `json:"task_id"`
		Status    string          `json:"task_status"`
		Error     string          `json:"task_status_msg"`
		Deduction json.RawMessage `json:"final_unit_deduction"`
		Result    struct {
			Videos []struct {
				URL      string          `json:"url"`
				Duration json.RawMessage `json:"duration"`
			} `json:"videos"`
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
		} `json:"task_result"`
	} `json:"data"`
}

// decodeResponse parses the kling response envelope, builds the resulting
// native.Result, and sums video durations (or image count) as billed units.
func decodeResponse(data []byte, method, action, id string) (native.Result, error) {
	var response klingResponse
	if err := json.Unmarshal(data, &response); err != nil || response.Code == nil {
		return native.Result{}, errors.New("invalid kling response")
	}
	if *response.Code != 0 {
		if method == http.MethodGet {
			return native.Result{}, errors.New("kling fetch rejected")
		}
		return native.Result{Status: "failed", Error: response.Message, Data: data}, nil
	}
	taskID := resolveKlingTaskID(response, method, id)
	if taskID == "" {
		return native.Result{}, errors.New("kling task ID missing")
	}
	status, err := taskStatus(response.Data.Status, method == http.MethodPost)
	if err != nil {
		return native.Result{}, err
	}
	data, err = annotateKlingData(data, action)
	if err != nil {
		return native.Result{}, err
	}
	r := native.Result{ID: taskID, Status: status, Error: response.Data.Error, Data: data}
	if status == "completed" {
		applyKlingCompletion(&r, response)
	}
	return r, nil
}

// resolveKlingTaskID picks the task ID from the response's data/top-level
// fields, falling back to the originally requested id for GET polls.
func resolveKlingTaskID(response klingResponse, method, id string) string {
	taskID := response.Data.ID
	if taskID == "" {
		taskID = response.TaskID
	}
	if taskID == "" && method == http.MethodGet {
		taskID = id
	}
	return taskID
}

// annotateKlingData re-marshals data with the resolved action recorded under
// "_v3_action" for use on the next poll.
func annotateKlingData(data []byte, action string) ([]byte, error) {
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil || saved == nil {
		return nil, errors.New("invalid kling response object")
	}
	saved["_v3_action"], _ = json.Marshal(action)
	return json.Marshal(saved)
}

// applyKlingCompletion fills in the result URL and billed units for a
// completed task, from video durations, image count, or unit deduction.
func applyKlingCompletion(r *native.Result, response klingResponse) {
	for _, video := range response.Data.Result.Videos {
		if r.URL == "" {
			r.URL = video.URL
		}
		r.Units += number(video.Duration)
	}
	if len(response.Data.Result.Videos) == 0 && len(response.Data.Result.Images) > 0 {
		r.URL = response.Data.Result.Images[0].URL
		r.Units = float64(len(response.Data.Result.Images))
	}
	if n := number(response.Data.Deduction); n > 0 && n < float64(math.MaxInt64) {
		r.Usage.CompletionTokens = int64(math.Ceil(n))
	}
}

func taskStatus(s string, submit bool) (string, error) {
	switch strings.ToLower(s) {
	case "submitted", "queued", "queueing":
		return "queued", nil
	case "":
		if submit {
			return "queued", nil
		}
	case "processing", "running":
		return "in_progress", nil
	case "succeed", "success", "succeeded", "completed":
		return "completed", nil
	case "failed", "failure", "error", "cancelled", "canceled":
		return "failed", nil
	}
	return "", errors.New("unknown kling task status")
}

func number(raw json.RawMessage) float64 {
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return 0
		}
		n, _ = strconv.ParseFloat(s, 64)
	}
	if n > 0 && !math.IsNaN(n) && !math.IsInf(n, 0) {
		return n
	}
	return 0
}

func invalid(message string) error { return &native.InvalidRequest{Err: errors.New(message)} }
