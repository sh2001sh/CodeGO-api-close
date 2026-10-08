package desktop

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func (s *Service) pricingHTTP(w http.ResponseWriter, r *http.Request) {
	d, ok := s.device(w, r, "account:read")
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		reply(w, nil, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	metadata, err := catalog.ReadMetadata(ctx, tx)
	if err != nil {
		reply(w, nil, err)
		return
	}
	rows, err := tx.Query(ctx, availableModels+`SELECT p.model,p.mode,p.input_per_mtok,p.output_per_mtok,p.cache_read_per_mtok,p.cache_write_per_mtok,p.per_request,p.rules,
	 ARRAY(SELECT DISTINCT group_name FROM available WHERE model=p.model ORDER BY group_name)
	 FROM v3_catalog.model_prices p WHERE EXISTS(SELECT 1 FROM available WHERE model=p.model) ORDER BY p.model`, d.UserID)
	if err != nil {
		reply(w, nil, err)
		return
	}
	items := []map[string]any{}
	models := []string{}
	supportedEndpoints := map[string]endpointInfo{}
	for rows.Next() {
		var model, mode string
		var input, output, cache, write, request int64
		var groups []string
		var rawRules json.RawMessage
		if err := rows.Scan(&model, &mode, &input, &output, &cache, &write, &request, &rawRules, &groups); err != nil {
			rows.Close()
			reply(w, nil, err)
			return
		}
		completion, cacheRatio, writeRatio := float64(0), float64(0), float64(0)
		if input > 0 {
			completion = float64(output) / float64(input)
			cacheRatio = float64(cache) / float64(input)
			writeRatio = float64(write) / float64(input)
		}
		quotaType := 0
		if mode == "per_request" {
			quotaType = 1
		}
		item := map[string]any{"model_name": model, "mode": mode, "input_per_mtok": input, "output_per_mtok": output, "per_request": request, "quota_type": quotaType, "model_ratio": float64(input) / 2e6, "model_price": float64(request) / 1e6, "completion_ratio": completion, "cache_ratio": cacheRatio, "create_cache_ratio": writeRatio, "owner_by": "", "enable_groups": groups, "supported_endpoint_types": []string{}, "billing_mode": mode, "pricing_available": true}
		var rules map[string]json.RawMessage
		if err := json.Unmarshal(rawRules, &rules); err != nil {
			rows.Close()
			reply(w, nil, err)
			return
		}
		if mode == "expression" {
			item["billing_mode"] = "tiered_expr"
			var expression string
			if value := rules["expression"]; len(value) > 0 {
				if err := json.Unmarshal(value, &expression); err != nil {
					rows.Close()
					reply(w, nil, err)
					return
				}
			}
			if expression == "" {
				if value := rules["expr"]; len(value) > 0 {
					if err := json.Unmarshal(value, &expression); err != nil {
						rows.Close()
						reply(w, nil, err)
						return
					}
				}
			}
			item["billing_expr"] = expression
		}
		for _, field := range []string{"image_ratio", "audio_ratio", "audio_completion_ratio"} {
			if value, ok := rules[field]; ok {
				item[field] = value
			}
		}
		if meta, vendor := metadata.Describe(model); meta != nil {
			item["description"], item["icon"], item["tags"], item["vendor_id"] = meta.Description, meta.Icon, meta.Tags, meta.VendorID
			endpoints, infos := pricingEndpoints(meta.Endpoints)
			item["supported_endpoint_types"] = endpoints
			for key, info := range infos {
				supportedEndpoints[key] = info
			}
			if vendor != nil {
				item["owner_by"] = vendor.Name
			}
		}
		items = append(items, item)
		models = append(models, model)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		reply(w, nil, err)
		return
	}
	rows, err = tx.Query(ctx, `SELECT name,description,multiplier::double precision FROM v3_catalog.groups WHERE name IN (SELECT v3_identity.allowed_groups($1)) ORDER BY name`, d.UserID)
	if err != nil {
		reply(w, nil, err)
		return
	}
	ratios := map[string]float64{}
	usable := map[string]string{}
	for rows.Next() {
		var name, desc string
		var ratio float64
		if err := rows.Scan(&name, &desc, &ratio); err != nil {
			rows.Close()
			reply(w, nil, err)
			return
		}
		ratios[name] = ratio
		usable[name] = desc
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		reply(w, nil, err)
		return
	}
	var auto []string
	if err := tx.QueryRow(ctx, `SELECT ARRAY(SELECT v3_identity.auto_groups($1))`, d.UserID).Scan(&auto); err != nil {
		reply(w, nil, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		reply(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": items, "available_models": models, "priced_models": models, "priced_model_details": items, "vendors": metadata.Vendors, "group_ratio": ratios, "usable_group": usable, "supported_endpoint": supportedEndpoints, "auto_groups": auto, "pricing_version": "v3"})
}
