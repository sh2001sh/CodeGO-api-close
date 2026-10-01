package legacy

import "strings"

// These published v2 request rates were multiplied by image count or video
// seconds at the media ingress. Preserve those units explicitly; ordinary
// tasks (for example Suno) keep their original price per call.
func legacyMediaUnit(model string) string {
	switch {
	case strings.HasPrefix(model, "dall-e-"), strings.HasPrefix(model, "imagen-"), strings.HasPrefix(model, "black-forest-labs/flux-"):
		return "image"
	case strings.HasPrefix(model, "veo-"):
		return "video_second"
	default:
		return ""
	}
}
