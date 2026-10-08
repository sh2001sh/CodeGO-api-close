package gateway

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// FastServiceTier recognizes Codex's current and legacy wire names. Other
// providers' speed fields and Anthropic service tiers have different semantics.
func FastServiceTier(req *Request) string {
	if !supportsFastServiceTier(req) {
		return ""
	}
	if req.hasParsedBody {
		return req.parsedServiceTier
	}
	tier := gjson.GetBytes(req.Body, "service_tier").Str
	if tier == "fast" || tier == "priority" {
		return tier
	}
	return ""
}

func supportsFastServiceTier(req *Request) bool {
	return req != nil && (req.Protocol == ProtocolResponses || req.Protocol == ProtocolOpenAIChat) &&
		(req.Path == "" || strings.HasSuffix(req.Path, "/responses") || strings.HasSuffix(req.Path, "/responses/compact") || strings.HasSuffix(req.Path, "/chat/completions"))
}

// Fast is explicitly priced, so the old premium-field filter must not silently
// remove it. Apply it after overrides to keep admission and upstream consistent.
func applyFastServiceTier(body []byte, req *Request, anthropic bool) ([]byte, error) {
	if !supportsFastServiceTier(req) {
		return body, nil
	}
	tier := FastServiceTier(req)
	if tier == "" {
		if outgoing := gjson.GetBytes(body, "service_tier").Str; outgoing == "fast" || outgoing == "priority" {
			return nil, invalidOverride("fast_mode_requires_client_opt_in")
		}
		return body, nil
	}
	// Inspect small top-level keys, rather than copying a possibly megabyte input
	// just to check that it exists.
	var hasInput bool
	gjson.GetBytes(body, "@keys").ForEach(func(_, key gjson.Result) bool {
		hasInput = key.Str == "input" || key.Str == "messages"
		return !hasInput
	})
	if anthropic || !hasInput {
		return nil, invalidOverride("fast_mode_unsupported")
	}
	if gjson.GetBytes(body, "service_tier").Str == tier {
		return body, nil
	}
	return sjson.SetBytes(body, "service_tier", tier)
}
