package catalogcontrol

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"

	"github.com/sh2001sh/new-api/v3/internal/billing/pricing"
	"github.com/sh2001sh/new-api/v3/internal/catalog"

	"github.com/jackc/pgx/v5"
)

type Group struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Multiplier  float64 `json:"multiplier"`
}
type Price struct {
	Model             string          `json:"model"`
	Mode              string          `json:"mode"`
	InputPerMTok      int64           `json:"input_per_mtok"`
	OutputPerMTok     int64           `json:"output_per_mtok"`
	CacheReadPerMTok  int64           `json:"cache_read_per_mtok"`
	CacheWritePerMTok int64           `json:"cache_write_per_mtok"`
	PerRequest        int64           `json:"per_request"`
	Rules             json.RawMessage `json:"rules"`
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT name,description,multiplier FROM v3_catalog.groups ORDER BY name`)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	items := make([]Group, 0)
	for rows.Next() {
		var g Group
		if err = rows.Scan(&g.Name, &g.Description, &g.Multiplier); err != nil {
			s.dbError(w, err)
			return
		}
		items = append(items, g)
	}
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, items)
}

func (s *Server) saveGroup(w http.ResponseWriter, r *http.Request) {
	var g Group
	if !decode(w, r, &g) {
		return
	}
	name := r.PathValue("name")
	if name == "" || len(name) > 255 || (g.Name != "" && g.Name != name) || g.Multiplier < 0 || g.Multiplier >= 1000000 || math.IsNaN(g.Multiplier) || math.IsInf(g.Multiplier, 0) {
		fail(w, 400, "invalid_group", "Invalid group name or multiplier")
		return
	}
	_, err := s.pool.Exec(r.Context(), `INSERT INTO v3_catalog.groups(name,description,multiplier) VALUES($1,$2,$3) ON CONFLICT(name) DO UPDATE SET description=excluded.description,multiplier=excluded.multiplier`, name, g.Description, g.Multiplier)
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, nil)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM v3_catalog.groups WHERE name=$1`, r.PathValue("name"))
	if err != nil {
		s.dbError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.dbError(w, pgx.ErrNoRows)
		return
	}
	respond(w, 200, nil)
}

func (s *Server) listPrices(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT model,mode,input_per_mtok,output_per_mtok,cache_read_per_mtok,cache_write_per_mtok,per_request,rules FROM v3_catalog.model_prices ORDER BY model`)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	items := make([]Price, 0)
	for rows.Next() {
		var p Price
		if err = rows.Scan(&p.Model, &p.Mode, &p.InputPerMTok, &p.OutputPerMTok, &p.CacheReadPerMTok, &p.CacheWritePerMTok, &p.PerRequest, &p.Rules); err != nil {
			s.dbError(w, err)
			return
		}
		items = append(items, p)
	}
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, items)
}

func (s *Server) savePrice(w http.ResponseWriter, r *http.Request) {
	var p Price
	if !decode(w, r, &p) {
		return
	}
	model := r.PathValue("model")
	if p.Mode == "" {
		p.Mode = "per_token"
	}
	if len(p.Rules) == 0 {
		p.Rules = json.RawMessage(`{}`)
	}
	var rules map[string]json.RawMessage
	if model == "" || len(model) > 255 || (p.Model != "" && p.Model != model) || (p.Mode != "per_token" && p.Mode != "per_request" && p.Mode != "expression") || p.InputPerMTok < 0 || p.OutputPerMTok < 0 || p.CacheReadPerMTok < 0 || p.CacheWritePerMTok < 0 || p.PerRequest < 0 || json.Unmarshal(p.Rules, &rules) != nil || rules == nil {
		fail(w, 400, "invalid_price", "Invalid model, price mode, amount or rules")
		return
	}
	var priceRules map[string]any
	decoder := json.NewDecoder(bytes.NewReader(p.Rules))
	decoder.UseNumber()
	if err := decoder.Decode(&priceRules); err != nil {
		fail(w, 400, "invalid_price", "Invalid pricing rules")
		return
	}
	if err := pricing.Validate(catalog.Price{Model: model, Mode: p.Mode, InputPerMTok: p.InputPerMTok, OutputPerMTok: p.OutputPerMTok, CacheReadPerMTok: p.CacheReadPerMTok, CacheWritePerMTok: p.CacheWritePerMTok, PerRequest: p.PerRequest, Rules: priceRules}); err != nil {
		fail(w, 400, "invalid_price", err.Error())
		return
	}
	_, err := s.pool.Exec(r.Context(), `INSERT INTO v3_catalog.model_prices(model,mode,input_per_mtok,output_per_mtok,cache_read_per_mtok,cache_write_per_mtok,per_request,rules) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(model) DO UPDATE SET mode=excluded.mode,input_per_mtok=excluded.input_per_mtok,output_per_mtok=excluded.output_per_mtok,cache_read_per_mtok=excluded.cache_read_per_mtok,cache_write_per_mtok=excluded.cache_write_per_mtok,per_request=excluded.per_request,rules=excluded.rules`, model, p.Mode, p.InputPerMTok, p.OutputPerMTok, p.CacheReadPerMTok, p.CacheWritePerMTok, p.PerRequest, p.Rules)
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, nil)
}

func (s *Server) deletePrice(w http.ResponseWriter, r *http.Request) {
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM v3_catalog.model_prices WHERE model=$1`, r.PathValue("model"))
	if err != nil {
		s.dbError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.dbError(w, pgx.ErrNoRows)
		return
	}
	respond(w, 200, nil)
}
