package catalogcontrol

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func (s *Server) metadataListPrefills(w http.ResponseWriter, r *http.Request) {
	data, err := s.readMetadata(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	items := make([]catalog.PrefillGroup, 0)
	for _, item := range data.PrefillGroups {
		if r.URL.Query().Get("type") == "" || r.URL.Query().Get("type") == item.Type {
			items = append(items, item)
		}
	}
	respond(w, 200, items)
}

func validPrefillItems(kind string, raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	switch kind {
	case "model", "tag":
		var items []string
		if json.Unmarshal(raw, &items) != nil || items == nil {
			return false
		}
		for _, item := range items {
			if strings.TrimSpace(item) == "" {
				return false
			}
		}
		return true
	case "endpoint":
		return json.Valid(raw) && len(raw) > 0 && (raw[0] == '{' || raw[0] == '[')
	default:
		return false
	}
}

func (s *Server) metadataSavePrefill(w http.ResponseWriter, r *http.Request) {
	var item catalog.PrefillGroup
	if !decode(w, r, &item) {
		return
	}
	id, ok := metadataWriteID(w, r, item.ID)
	if !ok {
		return
	}
	item.Name, item.Type = strings.TrimSpace(item.Name), strings.TrimSpace(item.Type)
	if item.Name == "" || len(item.Name) > 128 || !validPrefillItems(item.Type, item.Items) {
		fail(w, 400, "invalid_prefill", "Prefill requires a name, valid type and reusable items")
		return
	}
	var err error
	var created, updated time.Time
	if id == 0 {
		err = s.pool.QueryRow(r.Context(), `INSERT INTO v3_catalog.prefill_groups(name,type,description,items)
			VALUES($1,$2,$3,$4) RETURNING id,created_at,updated_at`, item.Name, item.Type, item.Description, item.Items).Scan(&id, &created, &updated)
	} else {
		err = s.pool.QueryRow(r.Context(), `UPDATE v3_catalog.prefill_groups SET name=$2,type=$3,description=$4,items=$5
			WHERE id=$1 AND deleted_at IS NULL RETURNING id,created_at,updated_at`, id, item.Name, item.Type, item.Description, item.Items).Scan(&id, &created, &updated)
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	item.ID, item.CreatedTime, item.UpdatedTime = id, created.Unix(), updated.Unix()
	respond(w, 200, item)
}

func (s *Server) metadataDeletePrefill(w http.ResponseWriter, r *http.Request) {
	s.metadataDelete(w, r, "prefill_groups")
}
