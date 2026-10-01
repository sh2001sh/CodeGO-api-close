package vertex

import (
	"errors"
	"net/url"
	"strings"
)

var claudeModels = map[string]string{
	"claude-3-sonnet-20240229": "claude-3-sonnet@20240229", "claude-3-opus-20240229": "claude-3-opus@20240229",
	"claude-3-haiku-20240307": "claude-3-haiku@20240307", "claude-3-5-sonnet-20240620": "claude-3-5-sonnet@20240620",
	"claude-3-5-sonnet-20241022": "claude-3-5-sonnet-v2@20241022", "claude-3-7-sonnet-20250219": "claude-3-7-sonnet@20250219",
	"claude-sonnet-4-20250514": "claude-sonnet-4@20250514", "claude-opus-4-20250514": "claude-opus-4@20250514",
	"claude-opus-4-1-20250805": "claude-opus-4-1@20250805", "claude-sonnet-4-5-20250929": "claude-sonnet-4-5@20250929",
	"claude-haiku-4-5-20251001": "claude-haiku-4-5@20251001", "claude-opus-4-5-20251101": "claude-opus-4-5@20251101",
}

func validSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '@':
		default:
			return false
		}
	}
	return true
}

func modelURL(base string, c Credentials, clientModel, model string, f family, stream bool) (*url.URL, error) {
	region := c.region(clientModel)
	if err := validateURLParts(region, c.ProjectID, model, f); err != nil {
		return nil, err
	}
	version := "v1"
	if f == openSource {
		if c.ProjectID == "" {
			return nil, errors.New("vertex: open-source models require a project")
		}
		version = "v1beta1"
	}
	if strings.TrimSpace(base) == "" {
		host := "aiplatform.googleapis.com"
		if region != "global" {
			host = region + "-" + host
		}
		base = "https://" + host
	}
	u, err := endpointURL(strings.TrimRight(strings.TrimSpace(base), "/"))
	if err != nil {
		return nil, err
	}
	path := strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{"/v1", "/v1beta1"} {
		path = strings.TrimSuffix(path, suffix)
	}
	path += "/" + version
	if c.ProjectID != "" {
		path += "/projects/" + c.ProjectID + "/locations/" + region
	}
	path += modelActionPath(model, f, stream, u)
	u.Path, u.RawPath = path, ""
	return u, nil
}

// validateURLParts rejects region, project or model segments that cannot
// appear verbatim in a Vertex URL path.
func validateURLParts(region, projectID, model string, f family) error {
	validModel := validSegment(model)
	if f == openSource {
		// OpenAPI models may include a publisher (e.g. meta/llama-3.3-...);
		// their ID stays in the JSON body and never becomes an endpoint path.
		parts := strings.Split(model, "/")
		validModel = len(parts) <= 2
		for _, part := range parts {
			validModel = validModel && validSegment(part)
		}
	}
	if !validSegment(region) || (projectID != "" && !validSegment(projectID)) || !validModel {
		return errors.New("vertex: invalid project, region or model")
	}
	return nil
}

// modelActionPath builds the publisher/model/action path suffix and, for
// streaming non-open-source requests, sets the SSE query parameter on u.
func modelActionPath(model string, f family, stream bool, u *url.URL) string {
	if f == openSource {
		return "/endpoints/openapi/chat/completions"
	}
	publisher, action := "google", "generateContent"
	if stream {
		action = "streamGenerateContent"
	}
	if f == claude {
		publisher, action = "anthropic", "rawPredict"
		if mapped := claudeModels[model]; mapped != "" {
			model = mapped
		}
		if stream {
			action = "streamRawPredict"
		}
	}
	if f == imagen {
		action = "predict"
	}
	if stream {
		query := u.Query()
		query.Set("alt", "sse")
		u.RawQuery = query.Encode()
	}
	return "/publishers/" + publisher + "/models/" + model + ":" + action
}
