package catalogcontrol

import (
	"context"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type metadataSyncRequest struct {
	Locale    string `json:"locale"`
	Overwrite []struct {
		ModelName string   `json:"model_name"`
		Fields    []string `json:"fields"`
	} `json:"overwrite"`
}

type metadataSyncResult struct {
	CreatedModels  int                `json:"created_models"`
	CreatedVendors int                `json:"created_vendors"`
	UpdatedModels  int                `json:"updated_models"`
	SkippedModels  []string           `json:"skipped_models"`
	CreatedList    []string           `json:"created_list"`
	UpdatedList    []string           `json:"updated_list"`
	Source         metadataSyncSource `json:"source"`
}

func metadataMissingNames(data catalog.MetadataSnapshot, bindings []metadataBinding, upstream map[string]metadataUpstreamModel) []string {
	existing := make(map[string]bool)
	for _, model := range data.Models {
		existing[model.ModelName] = true
	}
	missing := make([]string, 0)
	for _, binding := range bindings {
		if existing[binding.Model] || (len(missing) > 0 && missing[len(missing)-1] == binding.Model) {
			continue
		}
		if upstream != nil {
			if _, known := upstream[binding.Model]; !known {
				continue
			}
		}
		missing = append(missing, binding.Model)
	}
	return missing
}

func (s *Server) metadataSync(w http.ResponseWriter, r *http.Request) {
	var req metadataSyncRequest
	if !decode(w, r, &req) {
		return
	}
	if len(req.Overwrite) > 2000 {
		fail(w, 400, "invalid_sync", "Too many overwrite entries")
		return
	}
	for _, overwrite := range req.Overwrite {
		if strings.TrimSpace(overwrite.ModelName) == "" {
			fail(w, 400, "invalid_sync", "Overwrite requires a model name")
			return
		}
		for _, field := range overwrite.Fields {
			switch strings.ToLower(strings.TrimSpace(field)) {
			case "description", "icon", "tags", "vendor", "name_rule", "status":
			default:
				fail(w, 400, "invalid_sync", "Unsupported overwrite field")
				return
			}
		}
	}
	source, err := metadataSource(req.Locale)
	if err != nil {
		fail(w, 502, "metadata_sync_failed", err.Error())
		return
	}
	models, vendors, err := fetchMetadata(r.Context(), source)
	if err != nil {
		fail(w, 502, "metadata_sync_failed", err.Error())
		return
	}
	result, err := s.applyMetadataSync(r.Context(), req, source, models, vendors)
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, result)
}

func (s *Server) applyMetadataSync(ctx context.Context, req metadataSyncRequest, source metadataSyncSource, models map[string]metadataUpstreamModel, vendors map[string]metadataUpstreamVendor) (metadataSyncResult, error) {
	result := metadataSyncResult{SkippedModels: []string{}, CreatedList: []string{}, UpdatedList: []string{}, Source: source}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('catalog_metadata_sync',0))`); err != nil {
		return result, err
	}
	data, err := catalog.ReadMetadata(ctx, tx)
	if err != nil {
		return result, err
	}
	bindings, err := readMetadataBindings(ctx, tx)
	if err != nil {
		return result, err
	}
	missing := metadataMissingNames(data, bindings, nil)
	for _, name := range missing {
		upstream, found := models[name]
		if !found {
			result.SkippedModels = append(result.SkippedModels, name)
			continue
		}
		vendorID, err := syncMetadataVendor(ctx, tx, &data, upstream.VendorName, vendors, &result)
		if err != nil {
			return result, err
		}
		item := catalog.ModelMetadata{ModelName: name, Description: upstream.Description, Icon: upstream.Icon,
			Tags: upstream.Tags, VendorID: vendorID, NameRule: upstream.NameRule, Status: metadataSyncStatus(upstream.Status, 1), SyncOfficial: 1}
		if err = insertMetadataModel(ctx, tx, &item); err != nil {
			return result, err
		}
		data.Models = append(data.Models, item)
		result.CreatedModels++
		result.CreatedList = append(result.CreatedList, name)
	}
	for _, overwrite := range req.Overwrite {
		upstream, found := models[overwrite.ModelName]
		if !found {
			continue
		}
		for i := range data.Models {
			item := &data.Models[i]
			if item.ModelName != overwrite.ModelName || item.SyncOfficial == 0 || len(overwrite.Fields) == 0 {
				continue
			}
			if err = overwriteMetadataModel(ctx, tx, item, overwrite.Fields, upstream, &data, vendors, &result); err != nil {
				return result, err
			}
			result.UpdatedModels++
			result.UpdatedList = append(result.UpdatedList, item.ModelName)
			break
		}
	}
	return result, tx.Commit(ctx)
}

func metadataSyncStatus(value, fallback int) int {
	if value == 0 && fallback != 0 {
		return fallback
	}
	return value
}
