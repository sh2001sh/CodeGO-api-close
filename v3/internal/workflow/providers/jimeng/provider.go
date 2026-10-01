package jimeng

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type Provider struct{ client *http.Client }

func New(client *http.Client) *Provider { return &Provider{native.Client(client)} }

var _ native.Adapter = (*Provider)(nil)

func (p *Provider) Submit(ctx context.Context, target gateway.Target, input native.Submit) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	body, key, frames, err := requestBody(input, target.UpstreamModel)
	if err != nil {
		return native.Result{}, &native.InvalidRequest{Err: err}
	}
	return p.request(ctx, target, "CVSync2AsyncSubmitTask", "", key, frames, body)
}

func (p *Provider) Poll(ctx context.Context, target gateway.Target, task native.Task) (native.Result, error) {
	ctx = native.WithTarget(ctx, target)
	if task.UpstreamID == "" {
		return native.Result{}, invalid("missing upstream task ID")
	}
	key := target.UpstreamModel
	if key == "" {
		key = task.Model
	}
	var saved struct {
		Key    string `json:"_v3_req_key"`
		Frames int    `json:"_v3_frames"`
	}
	if json.Unmarshal(task.Data, &saved) == nil && saved.Key != "" {
		key = saved.Key
	}
	if key == "" {
		return native.Result{}, invalid("jimeng model required")
	}
	body, _ := json.Marshal(map[string]string{"req_key": key, "task_id": task.UpstreamID})
	return p.request(ctx, target, "CVSync2AsyncGetResult", task.UpstreamID, key, saved.Frames, body)
}

func (p *Provider) Content(context.Context, gateway.Target, native.Task) (*http.Response, error) {
	return nil, native.ErrContentUnsupported
}

func (p *Provider) request(ctx context.Context, target gateway.Target, action, id, key string, frames int, body []byte) (native.Result, error) {
	data, key, frames, err := p.send(ctx, target, action, id, key, frames, body)
	if err != nil {
		return native.Result{}, err
	}
	return decodeResponse(data, id, key, frames)
}

// send builds the signed (or relayed) upstream HTTP request, executes it,
// and returns the raw response body along with the possibly-overridden
// key/frames derived from the final request body.
func (p *Provider) send(ctx context.Context, target gateway.Target, action, id, key string, frames int, body []byte) ([]byte, string, int, error) {
	path := "/?Action=" + action + "&Version=2022-08-31"
	relay := strings.HasPrefix(target.Secret, "sk-")
	if relay {
		path = "/jimeng" + path
	}
	endpoint, err := native.Endpoint(target, "https://visual.volcengineapi.com", path)
	if err != nil {
		return nil, "", 0, &native.InvalidRequest{Err: err}
	}
	req, err := native.Request(ctx, http.MethodPost, endpoint, "", body)
	if err != nil {
		return nil, "", 0, &native.InvalidRequest{Err: err}
	}
	req.Header.Set("Accept", "application/json")
	if relay {
		req.Header.Set("Authorization", "Bearer "+target.Secret)
	}
	if err := native.Prepare(req); err != nil {
		return nil, "", 0, err
	}
	body, err = finalBody(req)
	if err != nil {
		return nil, "", 0, &native.InvalidRequest{Err: err}
	}
	if id == "" {
		var submitted struct {
			Key    string `json:"req_key"`
			Frames int    `json:"frames"`
		}
		if err := json.Unmarshal(body, &submitted); err != nil {
			return nil, "", 0, invalid("invalid overridden jimeng body")
		}
		if submitted.Key != "" {
			key = submitted.Key
		}
		if submitted.Frames > 1 {
			frames = submitted.Frames
		}
	}
	if !relay {
		parts := strings.Split(target.Secret, "|")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, "", 0, invalid("invalid jimeng credential: expected accessKey|secretKey")
		}
		if err := signRequest(req, body, strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), time.Now()); err != nil {
			return nil, "", 0, &native.InvalidRequest{Err: err}
		}
	}
	data, err := native.JSON(p.client, req)
	if err != nil {
		return nil, "", 0, err
	}
	return data, key, frames, nil
}

// decodeResponse parses the jimeng response envelope, builds the resulting
// native.Result, and computes billed units for completed tasks.
func decodeResponse(data []byte, id, key string, frames int) (native.Result, error) {
	var response struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Data    struct {
			ID           string  `json:"task_id"`
			Status       string  `json:"status"`
			URL          string  `json:"video_url"`
			Duration     float64 `json:"duration"`
			Frames       int     `json:"frames"`
			ResponseData string  `json:"resp_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.Code == nil {
		return native.Result{}, errors.New("invalid jimeng response")
	}
	if *response.Code != 10000 {
		if id != "" {
			return native.Result{}, errors.New("jimeng fetch rejected")
		}
		return native.Result{Status: "failed", Error: response.Message, Data: data}, nil
	}
	taskID := response.Data.ID
	if taskID == "" {
		taskID = id
	}
	if taskID == "" {
		return native.Result{}, errors.New("jimeng task ID missing")
	}
	status, err := taskStatus(response.Data.Status, id == "")
	if err != nil {
		return native.Result{}, err
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil || saved == nil {
		return native.Result{}, errors.New("invalid jimeng response object")
	}
	saved["_v3_req_key"], _ = json.Marshal(key)
	saved["_v3_frames"], _ = json.Marshal(frames)
	data, err = json.Marshal(saved)
	if err != nil {
		return native.Result{}, err
	}
	r := native.Result{ID: taskID, Status: status, URL: response.Data.URL, Data: data}
	if status == "failed" {
		r.Error = response.Message
	}
	if status == "completed" {
		r.Units = jimengUnits(response.Data.Duration, response.Data.Frames, response.Data.ResponseData, frames)
	}
	return r, nil
}

// jimengUnits computes billed duration (in seconds) from the response's own
// duration/frame count, falling back to the embedded resp_data details, and
// finally to the frame count known from the original request.
func jimengUnits(duration float64, respFrames int, responseData string, frames int) float64 {
	units := duration
	if units <= 0 && respFrames > 1 {
		units = float64(respFrames-1) / 24
	}
	if units <= 0 {
		var details struct {
			Duration float64 `json:"duration"`
			Frames   int     `json:"frames"`
		}
		if json.Unmarshal([]byte(responseData), &details) == nil {
			units = details.Duration
			if units <= 0 && details.Frames > 1 {
				units = float64(details.Frames-1) / 24
			}
		}
	}
	if units <= 0 && frames > 1 {
		units = float64(frames-1) / 24
	}
	return units
}

func taskStatus(s string, submit bool) (string, error) {
	switch strings.ToLower(s) {
	case "in_queue", "queued", "submitted":
		return "queued", nil
	case "":
		if submit {
			return "queued", nil
		}
	case "generating", "running", "processing", "in_progress":
		return "in_progress", nil
	case "done", "success", "completed":
		return "completed", nil
	case "failed", "failure", "error", "cancelled", "canceled", "not_found", "expired":
		return "failed", nil
	}
	return "", errors.New("unknown jimeng task status")
}

func invalid(message string) error { return &native.InvalidRequest{Err: errors.New(message)} }
