package gemini

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/workflow/native"
)

type veoOperation struct {
	Name  string `json:"name"`
	Done  bool   `json:"done"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Response struct {
		Filtered        int             `json:"raiMediaFilteredCount"`
		FilteredReasons []string        `json:"raiMediaFilteredReasons"`
		Duration        json.RawMessage `json:"durationSeconds"`
		Videos          []video         `json:"videos"`
		Bytes           string          `json:"bytesBase64Encoded"`
		Encoding        string          `json:"encoding"`
		Video           string          `json:"video"`
		Generated       struct {
			Videos []struct {
				Video video `json:"video"`
			} `json:"generatedVideos"`
		} `json:"generateVideoResponse"`
	} `json:"response"`
	Usage struct {
		Duration json.RawMessage `json:"duration"`
	} `json:"usage"`
}

// ParseResult accepts both Gemini URI results and Vertex inline/video results.
func ParseResult(data []byte, fallback string) (native.Result, error) {
	var op veoOperation
	if json.Unmarshal(data, &op) != nil {
		return native.Result{}, errors.New("invalid Veo operation response")
	}
	r := native.Result{ID: op.Name, Status: "in_progress", Data: append(json.RawMessage(nil), data...)}
	if r.ID == "" {
		r.ID = fallback
	}
	if op.Error != nil {
		r.Status = "failed"
		r.Error = op.Error.Message
		if r.Error == "" {
			r.Error = "Veo operation failed"
		}
		return r, nil
	}
	if !ValidOperation(r.ID) {
		return native.Result{}, errors.New("veo operation name missing or invalid")
	}
	if !op.Done {
		return r, nil
	}
	videos := collectVideos(op)
	if len(videos) == 0 {
		if op.Response.Filtered > 0 {
			r.Status = "failed"
			r.Error = "Veo output was filtered"
			if len(op.Response.FilteredReasons) > 0 {
				r.Error = op.Response.FilteredReasons[0]
			}
			return r, nil
		}
		return native.Result{}, errors.New("completed Veo operation has no video")
	}
	r.Status = "completed"
	r.URL = videos[0].url()
	if r.URL == "" {
		return native.Result{}, errors.New("veo video content missing")
	}
	r.Units = veoUnits(op, videos)
	return r, nil
}

// collectVideos gathers videos from the generateVideoResponse, videos, and
// inline bytes/video fields, in that order of precedence.
func collectVideos(op veoOperation) []video {
	var videos []video
	for _, generated := range op.Response.Generated.Videos {
		videos = append(videos, generated.Video)
	}
	videos = append(videos, op.Response.Videos...)
	if len(videos) == 0 && (op.Response.Bytes != "" || op.Response.Video != "") {
		encoded := op.Response.Bytes
		if encoded == "" {
			encoded = op.Response.Video
		}
		videos = append(videos, video{Bytes: encoded, Encoding: op.Response.Encoding})
	}
	return videos
}

// veoUnits sums each video's own duration when all are known, otherwise
// falls back to the operation-level usage duration or per-video duration.
func veoUnits(op veoOperation, videos []video) float64 {
	var units float64
	allDurations := true
	for _, v := range videos {
		duration := numeric(v.Duration)
		if duration <= 0 {
			allDurations = false
			break
		}
		units += duration
	}
	if allDurations {
		return units
	}
	units = numeric(op.Usage.Duration)
	if units <= 0 {
		units = numeric(op.Response.Duration) * float64(len(videos))
	}
	return units
}

type video struct {
	URI      string          `json:"uri"`
	GCS      string          `json:"gcsUri"`
	Bytes    string          `json:"bytesBase64Encoded"`
	MIME     string          `json:"mimeType"`
	Encoding string          `json:"encoding"`
	Duration json.RawMessage `json:"durationSeconds"`
}

func (v video) url() string {
	if v.URI != "" {
		return v.URI
	}
	if v.GCS != "" {
		return v.GCS
	}
	if v.Bytes == "" {
		return ""
	}
	kind := v.MIME
	if kind == "" {
		kind = v.Encoding
	}
	if kind == "" {
		kind = "mp4"
	}
	if kind == "mp4" || kind == "webm" || kind == "mov" {
		kind = "video/" + kind
	}
	return "data:" + kind + ";base64," + v.Bytes
}

func numeric(raw json.RawMessage) float64 {
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
