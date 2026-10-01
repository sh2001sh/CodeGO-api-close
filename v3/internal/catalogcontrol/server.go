// Package catalogcontrol provides PostgreSQL-backed catalog administration.
package catalogcontrol

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type Server struct {
	pool *pgxpool.Pool
	enc  catalog.Encrypter
	log  *slog.Logger
}

func New(pool *pgxpool.Pool, enc catalog.Encrypter, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{pool: pool, enc: enc, log: log}
}

// defaultAdminMiddleware rejects every request. It is used when Register is
// called without an administrator middleware, so a missing middleware never
// accidentally exposes credentials or configuration.
func defaultAdminMiddleware(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fail(w, 403, "forbidden", "Administrator authorization is required")
	})
}

// routeTable returns the full set of catalog control routes, including
// metadata routes.
func (s *Server) routeTable() map[string]http.HandlerFunc {
	routes := map[string]http.HandlerFunc{
		"GET /api/catalog/channels":                                  s.listChannels,
		"POST /api/catalog/channels":                                 s.saveChannel,
		"GET /api/catalog/channels/{id}":                             s.getChannel,
		"PUT /api/catalog/channels/{id}":                             s.saveChannel,
		"DELETE /api/catalog/channels/{id}":                          s.deleteChannel,
		"GET /api/catalog/channels/{id}/credentials":                 s.listCredentials,
		"POST /api/catalog/channels/{id}/credentials":                s.saveCredential,
		"PUT /api/catalog/channels/{id}/credentials/{credential}":    s.saveCredential,
		"DELETE /api/catalog/channels/{id}/credentials/{credential}": s.deleteCredential,
		"GET /api/catalog/groups":                                    s.listGroups,
		"PUT /api/catalog/groups/{name}":                             s.saveGroup,
		"DELETE /api/catalog/groups/{name}":                          s.deleteGroup,
		"GET /api/catalog/prices":                                    s.listPrices,
		"PUT /api/catalog/prices/{model}":                            s.savePrice,
		"DELETE /api/catalog/prices/{model}":                         s.deletePrice,
		"GET /api/catalog/route-pools":                               s.listPools,
		"PUT /api/catalog/route-pools":                               s.savePool,
		"DELETE /api/catalog/route-pools/{id}":                       s.deletePool,
		"GET /api/settings":                                          s.listSettings,
		"PUT /api/settings/{key}":                                    s.saveSetting,
		"DELETE /api/settings/{key}":                                 s.deleteSetting,
		"GET /api/option/{$}":                                        s.legacyListSettings,
		"PUT /api/option/{$}":                                        s.saveLegacySetting,
		"GET /api/channel/{$}":                                       s.legacyListChannels,
		"GET /api/channel/search":                                    s.legacyListChannels,
		"GET /api/channel/{id}":                                      s.legacyGetChannel,
		"POST /api/channel/{$}":                                      s.legacySaveChannel,
		"PUT /api/channel/{$}":                                       s.legacySaveChannel,
		"DELETE /api/channel/{id}":                                   s.deleteChannel,
		"POST /api/channel/batch":                                    s.legacyDeleteChannelBatch,
		"DELETE /api/channel/disabled":                               s.legacyDeleteDisabledChannels,
		"POST /api/channel/batch/tag":                                s.legacyBatchSetChannelTag,
		"POST /api/channel/tag/disabled":                             s.legacyDisableTagChannels,
		"POST /api/channel/tag/enabled":                              s.legacyEnableTagChannels,
		"PUT /api/channel/tag":                                       s.legacyEditTagChannels,
		"GET /api/channel/tag/models":                                s.legacyGetTagModels,
		"POST /api/channel/copy/{id}":                                s.legacyCopyChannel,
		"GET /api/channel/test":                                      s.legacyTestChannels,
		"GET /api/channel/test/{id}":                                 s.legacyTestChannel,
		"GET /api/channel/fetch_models/{id}":                         s.legacyFetchModels,
		"POST /api/channel/fetch_models":                             s.legacyFetchModels,
		"GET /api/channel/models":                                    s.legacyListModels,
		"GET /api/channel/models_enabled":                            s.legacyListModels,
		"GET /api/group/{$}":                                         s.legacyGroups,
		"GET /api/route-pools/{$}":                                   s.listPools,
		"POST /api/route-pools/{$}":                                  s.savePool,
		"PUT /api/route-pools/{$}":                                   s.savePool,
		"DELETE /api/route-pools/{id}":                               s.deletePool,
	}
	s.registerMetadataRoutes(routes)
	return routes
}

// guardCrossOrigin rejects cross-origin, state-changing requests before they
// reach the protected handler. GET/HEAD requests are always allowed through.
func guardCrossOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || (u.Scheme != "https" && u.Scheme != "http") {
			fail(w, 403, "forbidden", "Cross-origin configuration changes are forbidden")
			return false
		}
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		fail(w, 403, "forbidden", "Cross-origin configuration changes are forbidden")
		return false
	}
	return true
}

// Register requires an administrator middleware. A missing middleware rejects
// every request rather than exposing credentials or configuration accidentally.
func (s *Server) Register(mux *http.ServeMux, admin func(http.Handler) http.Handler) {
	if admin == nil {
		admin = defaultAdminMiddleware
	}
	for pattern, handler := range s.routeTable() {
		protected := admin(handler)
		mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if !guardCrossOrigin(w, r) {
				return
			}
			protected.ServeHTTP(w, r)
		}))
	}
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		fail(w, 400, "invalid_payload", "Invalid JSON request body")
		return false
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		fail(w, 400, "invalid_payload", "Expected one JSON value")
		return false
	}
	return true
}

func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    any    `json:"data,omitempty"`
	}{Success: true, Data: data})
}

func fail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Code    string `json:"code"`
	}{Message: message, Code: code})
}

func (s *Server) dbError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		fail(w, 404, "not_found", "Resource does not exist")
	case errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23503"):
		fail(w, 409, "conflict", "Resource conflicts with existing records")
	case errors.As(err, &pgErr) && (pgErr.Code == "23514" || pgErr.Code == "22P02"):
		fail(w, 400, "invalid_value", "A field violates a catalog constraint")
	default:
		s.log.Error("catalog control operation failed", "err", err)
		fail(w, 503, "catalog_unavailable", "Catalog storage is unavailable")
	}
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		fail(w, 400, "invalid_id", "Expected a positive resource identifier")
		return 0, false
	}
	return id, true
}

func page(r *http.Request) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	if p > 1000000 {
		p = 1000000
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if n <= 0 {
		n = 20
	}
	if n > 100 {
		n = 100
	}
	return p, n
}
