package catalogcontrol

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Server) metadataListModels(w http.ResponseWriter, r *http.Request) {
	views, err := s.metadataModelViews(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	items := make([]metadataModelView, 0)
	counts := make(map[int64]int)
	q := r.URL.Query()
	for i := len(views) - 1; i >= 0; i-- {
		item := views[i]
		counts[item.VendorID]++
		if !metadataSearch(q.Get("keyword"), item.ModelName, item.Description) ||
			(q.Get("vendor") != "" && q.Get("vendor") != strconv.FormatInt(item.VendorID, 10)) ||
			!metadataFlagMatches(q.Get("status"), item.Status) || !metadataFlagMatches(q.Get("sync_official"), item.SyncOfficial) {
			continue
		}
		items = append(items, item)
	}
	result := metadataPage(items, r)
	result["vendor_counts"] = counts
	respond(w, 200, result)
}

func metadataFlagMatches(filter string, value int) bool {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "enabled", "yes", "true", "1":
		return value == 1
	case "disabled", "no", "false", "0":
		return value == 0
	default:
		return true
	}
}

func (s *Server) metadataGetModel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	items, err := s.metadataModelViews(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	for _, item := range items {
		if item.ID == id {
			respond(w, 200, item)
			return
		}
	}
	s.dbError(w, pgx.ErrNoRows)
}

func (s *Server) metadataSaveModel(w http.ResponseWriter, r *http.Request) {
	var view metadataModelView
	if !decode(w, r, &view) {
		return
	}
	item := view.ModelMetadata
	id, ok := metadataWriteID(w, r, item.ID)
	if !ok {
		return
	}
	if item.Status < 0 || item.Status > math.MaxInt32 {
		fail(w, 400, "invalid_model", "Model status must be nonnegative")
		return
	}
	var err error
	if r.Method == http.MethodPut && r.URL.Query().Get("status_only") == "true" {
		err = s.pool.QueryRow(r.Context(), `UPDATE v3_catalog.models SET status=$2
			WHERE id=$1 AND deleted_at IS NULL RETURNING id`, id, item.Status).Scan(&id)
	} else {
		item.ModelName = strings.TrimSpace(item.ModelName)
		if item.ModelName == "" || len(item.ModelName) > 255 || item.VendorID < 0 || item.SyncOfficial < 0 || item.SyncOfficial > math.MaxInt32 || item.NameRule < 0 || item.NameRule > 3 {
			fail(w, 400, "invalid_model", "Invalid model name, vendor, status or matching rule")
			return
		}
		var vendor any
		if item.VendorID != 0 {
			vendor = item.VendorID
		}
		args := []any{item.ModelName, item.Description, item.Icon, item.Tags, item.Endpoints, vendor, item.Status, item.SyncOfficial, item.NameRule}
		if id == 0 {
			err = s.pool.QueryRow(r.Context(), `INSERT INTO v3_catalog.models
				(model_name,description,icon,tags,endpoints,vendor_id,status,sync_official,name_rule)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, args...).Scan(&id)
		} else {
			args = append(args, id)
			err = s.pool.QueryRow(r.Context(), `UPDATE v3_catalog.models SET model_name=$1,description=$2,
				icon=$3,tags=$4,endpoints=$5,vendor_id=$6,status=$7,sync_official=$8,name_rule=$9
				WHERE id=$10 AND deleted_at IS NULL RETURNING id`, args...).Scan(&id)
		}
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	r.SetPathValue("id", strconv.FormatInt(id, 10))
	s.metadataGetModel(w, r)
}

func (s *Server) metadataDeleteModel(w http.ResponseWriter, r *http.Request) {
	s.metadataDelete(w, r, "models")
}

// Like v2, missing metadata means an enabled model has no exact stored name;
// pattern metadata still describes it in price and model discovery responses.
func (s *Server) metadataMissingModels(w http.ResponseWriter, r *http.Request) {
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
	respond(w, 200, metadataMissingNames(data, bindings, nil))
}
