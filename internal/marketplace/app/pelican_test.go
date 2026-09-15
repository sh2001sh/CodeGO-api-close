package app

import (
	"strings"
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
)

func TestSanitizePelicanSVGKeepsAnimationAndRemovesActiveContent(t *testing.T) {
	raw := "```html\n<html><body><svg xmlns=\"http://www.w3.org/2000/svg\" onload=\"alert(1)\"><script>alert(1)</script><rect width=\"10\" height=\"10\"><animate attributeName=\"x\" values=\"0;10\" dur=\"1s\" repeatCount=\"indefinite\" /></rect><a href=\"https://evil.example\"><text>bad link</text></a></svg></body></html>```"
	svg, err := sanitizePelicanSVG(raw)
	require.NoError(t, err)
	require.Contains(t, svg, "<animate")
	require.NotContains(t, strings.ToLower(svg), "script")
	require.NotContains(t, strings.ToLower(svg), "onload")
	require.NotContains(t, svg, "evil.example")
}

func TestSanitizePelicanSVGKeepsLocalPaintReferences(t *testing.T) {
	raw := `<html><body><svg xmlns="http://www.w3.org/2000/svg"><defs><linearGradient id="sky"><stop offset="0" stop-color="#123456"/></linearGradient><filter id="soft"><feGaussianBlur stdDeviation="2"/></filter></defs><rect width="100" height="100" fill="url(#sky)" style="filter: url('#soft')"/><style>.wheel { fill: url("#sky"); }</style></svg></body></html>`
	svg, err := sanitizePelicanSVG(raw)
	require.NoError(t, err)
	require.Contains(t, svg, `fill="url(#sky)"`)
	require.Contains(t, svg, `url(&#34;#sky&#34;)`)
}

func TestSanitizePelicanSVGRemovesExternalPaintReferences(t *testing.T) {
	raw := `<svg xmlns="http://www.w3.org/2000/svg"><rect fill="url(https://evil.example/paint.svg#x)"/><circle style="fill:url(data:image/svg+xml,bad)"/><path stroke="javascript:alert(1)"/><ellipse fill="url(#safe) url(https://evil.example/x)"/></svg>`
	svg, err := sanitizePelicanSVG(raw)
	require.NoError(t, err)
	require.NotContains(t, svg, "evil.example")
	require.NotContains(t, svg, "data:")
	require.NotContains(t, svg, "javascript:")
}

func TestOfficialGroupCannotEnableScheduledPelicanTest(t *testing.T) {
	enabled := true
	channel := &marketplaceschema.Channel{DeclaredModels: `["gpt-test"]`}
	group := &marketplaceschema.Group{SourceType: marketplacedomain.SourceTypeOfficial}
	err := applyPelicanProbeUpdate(channel, group, UpdateChannelRequest{PelicanProbeEnabled: &enabled})
	require.ErrorContains(t, err, "官方分组")
}

func TestSanitizePelicanSVGRejectsMissingAndOversizedSVG(t *testing.T) {
	_, err := sanitizePelicanSVG("<html>no svg</html>")
	require.Error(t, err)
	_, err = sanitizePelicanSVG("<svg><text>" + strings.Repeat("x", maxPelicanSVGBytes) + "</text></svg>")
	require.Error(t, err)
}

func TestValidatePelicanProbe(t *testing.T) {
	require.NoError(t, validatePelicanProbe(false, -1, "", nil))
	require.NoError(t, validatePelicanProbe(true, 60, "gpt-test", []string{"gpt-test"}))
	require.Error(t, validatePelicanProbe(true, 1440, "gpt-test", []string{"gpt-test"}))
	require.Error(t, validatePelicanProbe(true, 60, "missing", []string{"gpt-test"}))
}
