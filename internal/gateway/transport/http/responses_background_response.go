package http

import (
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
)

func backgroundJobResponse(job *gatewayschema.ResponsesBackgroundJob) any {
	if job == nil {
		return map[string]any{}
	}
	if job.FinalResponseCiphertext != "" {
		if raw, err := platformsecurity.DecryptSecret(job.FinalResponseCiphertext); err == nil {
			var response map[string]any
			if platformencoding.Unmarshal([]byte(raw), &response) == nil {
				return response
			}
		}
	}
	response := map[string]any{
		"id": job.ID, "object": "response", "created_at": job.CreatedAt.Unix(),
		"model": job.Model, "background": true, "status": job.Status,
		"output": []any{}, "error": nil,
	}
	if response["created_at"].(int64) <= 0 {
		response["created_at"] = time.Now().Unix()
	}
	if job.ErrorCiphertext != "" {
		if raw, err := platformsecurity.DecryptSecret(job.ErrorCiphertext); err == nil {
			var errorValue any
			if platformencoding.Unmarshal([]byte(raw), &errorValue) == nil {
				response["error"] = errorValue
			}
		}
	}
	return response
}
