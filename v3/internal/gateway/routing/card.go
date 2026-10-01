package routing

import (
	"encoding/json"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func cardRouteGroup(snap *catalog.Snapshot) string {
	var group string
	if json.Unmarshal(snap.Settings["blind_box_setting.multiplier_card_route_group"], &group) == nil && strings.TrimSpace(group) != "" {
		return strings.TrimSpace(group)
	}
	var setting struct {
		Group string `json:"multiplier_card_route_group"`
	}
	if json.Unmarshal(snap.Settings["blind_box_setting"], &setting) == nil && strings.TrimSpace(setting.Group) != "" {
		return strings.TrimSpace(setting.Group)
	}
	return "纯Pro号池"
}
