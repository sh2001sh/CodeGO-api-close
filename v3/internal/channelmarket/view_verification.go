package channelmarket

import (
	"bytes"
	"encoding/json"
	"time"
)

type VerificationView struct {
	ID              string      `json:"verification_id,omitempty"`
	Stage           string      `json:"verification_stage"`
	DetectorVersion string      `json:"verification_detector_version"`
	StartedAt       *time.Time  `json:"verification_started_at,omitempty"`
	CompletedAt     *time.Time  `json:"verification_completed_at,omitempty"`
	ModelResults    []ModelTest `json:"model_verification_results"`
}

func parseVerificationView(raw []byte) (VerificationView, error) {
	out := VerificationView{ModelResults: []ModelTest{}}
	var data struct {
		ID        string          `json:"id"`
		Stage     string          `json:"stage"`
		Version   string          `json:"detector_version"`
		Started   *time.Time      `json:"started_at"`
		Completed *time.Time      `json:"completed_at"`
		Results   json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return out, err
	}
	out.ID = data.ID
	out.Stage = data.Stage
	out.DetectorVersion = data.Version
	out.StartedAt = data.Started
	out.CompletedAt = data.Completed
	results := bytes.TrimSpace(data.Results)
	if len(results) == 0 || bytes.Equal(results, []byte(`{}`)) || bytes.Equal(results, []byte(`null`)) {
		return out, nil
	}
	if err := json.Unmarshal(results, &out.ModelResults); err != nil {
		return out, err
	}
	for i := range out.ModelResults {
		if out.ModelResults[i].Error != "" {
			out.ModelResults[i].Error = "上游连通性或响应验证失败"
		}
	}
	return out, nil
}
