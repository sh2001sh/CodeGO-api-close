package catalogcontrol

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

func (s *Server) metadataListVendors(w http.ResponseWriter, r *http.Request) {
	data, err := s.readMetadata(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	items := make([]catalog.VendorMetadata, 0)
	for i := len(data.Vendors) - 1; i >= 0; i-- {
		item := data.Vendors[i]
		if metadataSearch(r.URL.Query().Get("keyword"), item.Name, item.Description) {
			items = append(items, item)
		}
	}
	respond(w, 200, metadataPage(items, r))
}

func (s *Server) metadataGetVendor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	data, err := s.readMetadata(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	for _, item := range data.Vendors {
		if item.ID == id {
			respond(w, 200, item)
			return
		}
	}
	s.dbError(w, pgx.ErrNoRows)
}

func (s *Server) metadataSaveVendor(w http.ResponseWriter, r *http.Request) {
	var item catalog.VendorMetadata
	if !decode(w, r, &item) {
		return
	}
	id, ok := metadataWriteID(w, r, item.ID)
	if !ok {
		return
	}
	item.Name = strings.TrimSpace(item.Name)
	if item.Name == "" || len(item.Name) > 128 || item.Status < 0 || item.Status > math.MaxInt32 {
		fail(w, 400, "invalid_vendor", "Vendor name and nonnegative status are required")
		return
	}
	var err error
	if id == 0 {
		err = s.pool.QueryRow(r.Context(), `INSERT INTO v3_catalog.vendors(name,description,icon,status)
			VALUES($1,$2,$3,$4) RETURNING id`, item.Name, item.Description, item.Icon, item.Status).Scan(&id)
	} else {
		err = s.pool.QueryRow(r.Context(), `UPDATE v3_catalog.vendors SET name=$2,description=$3,icon=$4,status=$5
			WHERE id=$1 AND deleted_at IS NULL RETURNING id`, id, item.Name, item.Description, item.Icon, item.Status).Scan(&id)
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	r.SetPathValue("id", strconv.FormatInt(id, 10))
	s.metadataGetVendor(w, r)
}

func (s *Server) metadataDeleteVendor(w http.ResponseWriter, r *http.Request) {
	s.metadataDelete(w, r, "vendors")
}
