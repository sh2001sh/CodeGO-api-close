package catalogcontrol

import (
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type metadataConflictField struct {
	Field    string `json:"field"`
	Local    any    `json:"local"`
	Upstream any    `json:"upstream"`
}

type metadataConflict struct {
	ModelName string                  `json:"model_name"`
	Fields    []metadataConflictField `json:"fields"`
}

func (s *Server) metadataSyncPreview(w http.ResponseWriter, r *http.Request) {
	source, err := metadataSource(r.URL.Query().Get("locale"))
	if err != nil {
		fail(w, 502, "metadata_sync_failed", err.Error())
		return
	}
	models, _, err := fetchMetadata(r.Context(), source)
	if err != nil {
		fail(w, 502, "metadata_sync_failed", err.Error())
		return
	}
	data, err := s.readMetadata(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	bindings, err := s.metadataBindings(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	conflicts := make([]metadataConflict, 0)
	for _, local := range data.Models {
		upstream, found := models[local.ModelName]
		if !found || local.SyncOfficial == 0 {
			continue
		}
		var vendorName string
		for _, vendor := range data.Vendors {
			if vendor.ID == local.VendorID {
				vendorName = vendor.Name
				break
			}
		}
		fields := metadataCompareFields(local, upstream, vendorName)
		if len(fields) > 0 {
			conflicts = append(conflicts, metadataConflict{ModelName: local.ModelName, Fields: fields})
		}
	}
	respond(w, 200, map[string]any{"missing": metadataMissingNames(data, bindings, models), "conflicts": conflicts, "source": source})
}

func metadataCompareFields(local catalog.ModelMetadata, upstream metadataUpstreamModel, vendorName string) []metadataConflictField {
	fields := make([]metadataConflictField, 0)
	for _, pair := range []struct{ field, a, b string }{
		{"description", local.Description, upstream.Description}, {"icon", local.Icon, upstream.Icon},
		{"tags", local.Tags, upstream.Tags}, {"vendor", vendorName, upstream.VendorName},
	} {
		if strings.TrimSpace(pair.a) != strings.TrimSpace(pair.b) {
			fields = append(fields, metadataConflictField{pair.field, pair.a, pair.b})
		}
	}
	if local.NameRule != upstream.NameRule {
		fields = append(fields, metadataConflictField{"name_rule", local.NameRule, upstream.NameRule})
	}
	if value := metadataSyncStatus(upstream.Status, local.Status); local.Status != value {
		fields = append(fields, metadataConflictField{"status", local.Status, value})
	}
	return fields
}
