package catalogcontrol

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type PoolMember struct {
	ChannelID          int64                  `json:"channel_id"`
	Priority           int                    `json:"priority"`
	Weight             int                    `json:"weight"`
	LegacyID           int64                  `json:"legacy_id,omitempty"`
	CostMultiplier     json.Number            `json:"cost_multiplier"`
	ModelCostOverrides map[string]json.Number `json:"model_cost_overrides"`
	FaultDomain        string                 `json:"fault_domain"`
	Enabled            *bool                  `json:"enabled,omitempty"`
	DeletedAt          *time.Time             `json:"deleted_at,omitempty"`
}
type RoutePool struct {
	ID               int64        `json:"id"`
	Group            string       `json:"group"`
	Model            string       `json:"model"`
	Strategy         string       `json:"strategy"`
	Enabled          bool         `json:"enabled"`
	Members          []PoolMember `json:"members"`
	Name             string       `json:"name"`
	ModelScope       string       `json:"model_scope"`
	AutoDiscover     bool         `json:"auto_discover"`
	MultiplierWeight int          `json:"multiplier_weight"`
	TTFTWeight       int          `json:"ttft_weight"`
	CacheWeight      int          `json:"cache_weight"`
	SuccessWeight    int          `json:"success_weight"`
	DeletedAt        *time.Time   `json:"deleted_at,omitempty"`
}

func (s *Server) listPools(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT p.id,p.group_name,p.model,p.strategy,p.enabled,p.name,p.model_scope,p.auto_discover,p.multiplier_weight,p.ttft_weight,p.cache_weight,p.success_weight,p.deleted_at,
	coalesce((SELECT jsonb_agg(jsonb_build_object('channel_id',m.channel_id,'priority',m.priority,'weight',m.weight,'legacy_id',m.legacy_id,'cost_multiplier',m.cost_multiplier,'model_cost_overrides',m.model_cost_overrides,'fault_domain',m.fault_domain,'enabled',m.enabled,'deleted_at',m.deleted_at) ORDER BY m.channel_id) FROM v3_catalog.route_pool_members m WHERE m.pool_id=p.id),'[]'::jsonb) FROM v3_catalog.route_pools p ORDER BY p.id`)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	items := make([]RoutePool, 0)
	for rows.Next() {
		var p RoutePool
		if err = rows.Scan(&p.ID, &p.Group, &p.Model, &p.Strategy, &p.Enabled, &p.Name, &p.ModelScope, &p.AutoDiscover, &p.MultiplierWeight, &p.TTFTWeight, &p.CacheWeight, &p.SuccessWeight, &p.DeletedAt, &p.Members); err != nil {
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

func (s *Server) savePool(w http.ResponseWriter, r *http.Request) {
	var p RoutePool
	if !decode(w, r, &p) {
		return
	}
	if err := normalizePool(&p); err != nil {
		fail(w, 400, "invalid_pool", "Invalid group, model, strategy or members")
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	if err = writePool(r.Context(), tx, &p); err != nil {
		s.dbError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, map[string]any{"id": p.ID})
}

func (s *Server) deletePool(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM v3_catalog.route_pools WHERE id=$1`, id)
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
