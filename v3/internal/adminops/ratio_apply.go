package adminops

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
	"github.com/sh2001sh/new-api/v3/internal/catalogcontrol"
)

func (s *Server) ratioApplyHTTP(w http.ResponseWriter, r *http.Request, _ Actor) {
	var in struct {
		Prices []catalogcontrol.Price `json:"prices"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Prices) == 0 || len(in.Prices) > 10000 {
		fail(w, 400, "invalid_prices", "Expected between 1 and 10000 model prices")
		return
	}
	seen := map[string]bool{}
	for i := range in.Prices {
		p := &in.Prices[i]
		if p.Mode == "" {
			p.Mode = "per_token"
		}
		if len(p.Rules) == 0 {
			p.Rules = json.RawMessage(`{}`)
		}
		var rules map[string]any
		decoder := json.NewDecoder(bytes.NewReader(p.Rules))
		decoder.UseNumber()
		if decoder.Decode(&rules) != nil || rules == nil || strings.TrimSpace(p.Model) == "" || len(p.Model) > 255 || seen[p.Model] {
			fail(w, 400, "invalid_price", "Invalid or duplicate model price")
			return
		}
		seen[p.Model] = true
		if err := pricing.Validate(catalog.Price{Model: p.Model, Mode: p.Mode, InputPerMTok: p.InputPerMTok, OutputPerMTok: p.OutputPerMTok, CacheReadPerMTok: p.CacheReadPerMTok, CacheWritePerMTok: p.CacheWritePerMTok, PerRequest: p.PerRequest, Rules: rules}); err != nil {
			fail(w, 400, "invalid_price", "Invalid model price or rules")
			return
		}
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	for _, p := range in.Prices {
		_, err = tx.Exec(r.Context(), `INSERT INTO v3_catalog.model_prices(model,mode,input_per_mtok,output_per_mtok,cache_read_per_mtok,cache_write_per_mtok,per_request,rules) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(model) DO UPDATE SET mode=excluded.mode,input_per_mtok=excluded.input_per_mtok,output_per_mtok=excluded.output_per_mtok,cache_read_per_mtok=excluded.cache_read_per_mtok,cache_write_per_mtok=excluded.cache_write_per_mtok,per_request=excluded.per_request,rules=excluded.rules`, p.Model, p.Mode, p.InputPerMTok, p.OutputPerMTok, p.CacheReadPerMTok, p.CacheWritePerMTok, p.PerRequest, p.Rules)
		if err != nil {
			s.dbError(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, map[string]any{"updated": len(in.Prices)})
}
