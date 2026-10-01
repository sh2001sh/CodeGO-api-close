package billing

import (
	"maps"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
	"github.com/sh2001sh/new-api/v3/internal/workflow/providers/gemini"
)

// Freeze Veo facts in a private copy of the admitted price. Existing task price
// serialization retains them; later routing/model changes cannot reprice work.
func freezeVideoPrice(req *gateway.Request, price catalog.Price, target gateway.Target) (catalog.Price, error) {
	unit, err := pricing.MediaUnit(price)
	if err != nil || unit != "video_second" {
		return price, err
	}
	model := target.UpstreamModel
	if model == "" {
		model = req.Model
	}
	if target.Provider != "gemini" && target.Provider != "vertex" && !strings.Contains(model, "veo-") {
		return price, nil
	}
	seconds, ppm, err := gemini.BillingParameters(req.Body, model)
	if err != nil {
		return price, err
	}
	price.Rules = maps.Clone(price.Rules)
	price.Rules["veo_duration_micros"] = seconds * 1_000_000
	price.Rules["veo_resolution_multiplier_ppm"] = ppm
	return price, nil
}
