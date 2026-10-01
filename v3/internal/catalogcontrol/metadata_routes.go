package catalogcontrol

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func (s *Server) registerMetadataRoutes(routes map[string]http.HandlerFunc) {
	for _, prefix := range []string{"/api/catalog/models", "/api/models"} {
		end := ""
		if prefix == "/api/models" {
			end = "/{$}"
		}
		routes["GET "+prefix+end] = s.metadataListModels
		routes["GET "+prefix+"/search"] = s.metadataListModels
		routes["GET "+prefix+"/missing"] = s.metadataMissingModels
		routes["GET "+prefix+"/{id}"] = s.metadataGetModel
		routes["POST "+prefix+end] = s.metadataSaveModel
		routes["PUT "+prefix+end] = s.metadataSaveModel
		routes["PUT "+prefix+"/{id}"] = s.metadataSaveModel
		routes["DELETE "+prefix+"/{id}"] = s.metadataDeleteModel
		if end != "" {
			routes["GET "+prefix] = s.metadataListModels
			routes["POST "+prefix] = s.metadataSaveModel
			routes["PUT "+prefix] = s.metadataSaveModel
		}
		routes["GET "+prefix+"/sync_upstream/preview"] = s.metadataSyncPreview
		routes["POST "+prefix+"/sync_upstream"] = s.metadataSync
	}
	for _, prefix := range []string{"/api/catalog/vendors", "/api/vendors"} {
		end := ""
		if prefix == "/api/vendors" {
			end = "/{$}"
		}
		routes["GET "+prefix+end] = s.metadataListVendors
		routes["GET "+prefix+"/search"] = s.metadataListVendors
		routes["GET "+prefix+"/{id}"] = s.metadataGetVendor
		routes["POST "+prefix+end] = s.metadataSaveVendor
		routes["PUT "+prefix+end] = s.metadataSaveVendor
		routes["PUT "+prefix+"/{id}"] = s.metadataSaveVendor
		routes["DELETE "+prefix+"/{id}"] = s.metadataDeleteVendor
		if end != "" {
			routes["GET "+prefix] = s.metadataListVendors
			routes["POST "+prefix] = s.metadataSaveVendor
			routes["PUT "+prefix] = s.metadataSaveVendor
		}
	}
	for _, prefix := range []string{"/api/catalog/prefill-groups", "/api/prefill_group"} {
		end := ""
		if prefix == "/api/prefill_group" {
			end = "/{$}"
		}
		routes["GET "+prefix+end] = s.metadataListPrefills
		routes["POST "+prefix+end] = s.metadataSavePrefill
		routes["PUT "+prefix+end] = s.metadataSavePrefill
		routes["PUT "+prefix+"/{id}"] = s.metadataSavePrefill
		routes["DELETE "+prefix+"/{id}"] = s.metadataDeletePrefill
		if end != "" {
			routes["GET "+prefix] = s.metadataListPrefills
			routes["POST "+prefix] = s.metadataSavePrefill
			routes["PUT "+prefix] = s.metadataSavePrefill
		}
	}
	// Keep the existing flattened native price shape for current clients.
	routes["GET /api/catalog/prices"] = s.metadataListPrices
}

func (s *Server) readMetadata(ctx context.Context) (catalog.MetadataSnapshot, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return catalog.MetadataSnapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	data, err := catalog.ReadMetadata(ctx, tx)
	if err != nil {
		return data, err
	}
	return data, tx.Commit(ctx)
}

func metadataWriteID(w http.ResponseWriter, r *http.Request, bodyID int64) (int64, bool) {
	if r.Method == http.MethodPost {
		if bodyID != 0 {
			fail(w, 400, "invalid_id", "New metadata must not specify an ID")
			return 0, false
		}
		return 0, true
	}
	if r.PathValue("id") != "" {
		id, ok := pathID(w, r, "id")
		if ok && bodyID != 0 && bodyID != id {
			fail(w, 400, "invalid_id", "Path and body IDs differ")
			return 0, false
		}
		return id, ok
	}
	if bodyID <= 0 {
		fail(w, 400, "invalid_id", "A positive metadata ID is required")
		return 0, false
	}
	return bodyID, true
}

func metadataSearch(keyword, name, description string) bool {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	return keyword == "" || strings.Contains(strings.ToLower(name), keyword) || strings.Contains(strings.ToLower(description), keyword)
}

func metadataPage[T any](items []T, r *http.Request) map[string]any {
	pageNumber, size := page(r)
	start := min((pageNumber-1)*size, len(items))
	end := min(start+size, len(items))
	return map[string]any{"items": items[start:end], "total": len(items), "page": pageNumber, "page_size": size}
}

func (s *Server) metadataDelete(w http.ResponseWriter, r *http.Request, table string) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	// table is an internal constant from the three concrete handlers.
	tag, err := s.pool.Exec(r.Context(), "UPDATE v3_catalog."+table+" SET deleted_at=now() WHERE id=$1 AND deleted_at IS NULL", id)
	if err == nil && tag.RowsAffected() == 0 {
		err = pgx.ErrNoRows
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, nil)
}
