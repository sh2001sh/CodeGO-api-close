package catalogcontrol

import (
	"context"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type metadataBoundChannel struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Type     int    `json:"type"`
	Provider string `json:"provider"`
}

type metadataModelView struct {
	catalog.ModelMetadata
	BoundChannels []metadataBoundChannel `json:"bound_channels,omitempty"`
	EnableGroups  []string               `json:"enable_groups,omitempty"`
	QuotaTypes    []int                  `json:"quota_types,omitempty"`
	MatchedModels []string               `json:"matched_models,omitempty"`
	MatchedCount  int                    `json:"matched_count,omitempty"`
}

type metadataBinding struct {
	Model string
	Group string
	metadataBoundChannel
}

func (s *Server) metadataBindings(ctx context.Context) ([]metadataBinding, error) {
	return readMetadataBindings(ctx, s.pool)
}

func readMetadataBindings(ctx context.Context, q catalog.MetadataQuerier) ([]metadataBinding, error) {
	rows, err := q.Query(ctx, `SELECT m.model,COALESCE(g.group_name,''),c.id,c.name,c.provider
		FROM v3_catalog.channel_models m JOIN v3_catalog.channels c ON c.id=m.channel_id
		LEFT JOIN v3_catalog.channel_groups g ON g.channel_id=c.id
		WHERE c.status='enabled' ORDER BY m.model,c.id,g.group_name`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (metadataBinding, error) {
		var binding metadataBinding
		err := row.Scan(&binding.Model, &binding.Group, &binding.ID, &binding.Name, &binding.Provider)
		for id, provider := range legacyProviderIDs() {
			if provider == binding.Provider && (binding.Type == 0 || id < binding.Type) {
				binding.Type = id
			}
		}
		return binding, err
	})
}

func (s *Server) metadataModelViews(ctx context.Context) ([]metadataModelView, error) {
	data, err := s.readMetadata(ctx)
	if err != nil {
		return nil, err
	}
	bindings, err := s.metadataBindings(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]metadataModelView, 0, len(data.Models))
	for _, item := range data.Models {
		view := metadataModelView{ModelMetadata: item}
		channels := map[int64]bool{}
		for _, binding := range bindings {
			if !catalog.MatchesModelName(item.ModelName, item.NameRule, binding.Model) {
				continue
			}
			if !slices.Contains(view.MatchedModels, binding.Model) {
				view.MatchedModels = append(view.MatchedModels, binding.Model)
			}
			if binding.Group != "" && !slices.Contains(view.EnableGroups, binding.Group) {
				view.EnableGroups = append(view.EnableGroups, binding.Group)
			}
			if !channels[binding.ID] {
				channels[binding.ID] = true
				view.BoundChannels = append(view.BoundChannels, binding.metadataBoundChannel)
			}
		}
		slices.Sort(view.EnableGroups)
		view.MatchedCount = len(view.MatchedModels)
		result = append(result, view)
	}
	return result, nil
}

type metadataPriceView struct {
	Price
	Metadata *catalog.ModelMetadata  `json:"metadata,omitempty"`
	Vendor   *catalog.VendorMetadata `json:"vendor,omitempty"`
}

func (s *Server) metadataListPrices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	metadata, err := catalog.ReadMetadata(ctx, tx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	rows, err := tx.Query(ctx, `SELECT model,mode,input_per_mtok,output_per_mtok,cache_read_per_mtok,
		cache_write_per_mtok,per_request,rules FROM v3_catalog.model_prices ORDER BY model`)
	if err != nil {
		s.dbError(w, err)
		return
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (metadataPriceView, error) {
		var item metadataPriceView
		err := row.Scan(&item.Model, &item.Mode, &item.InputPerMTok, &item.OutputPerMTok,
			&item.CacheReadPerMTok, &item.CacheWritePerMTok, &item.PerRequest, &item.Rules)
		item.Metadata, item.Vendor = metadata.Describe(item.Model)
		return item, err
	})
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, items)
}
