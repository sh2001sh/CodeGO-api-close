package security

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

type Authenticate func(*http.Request) (Actor, error)

// Register uses a distinct namespace. Legacy channel-market compatibility can
// delegate to Handler without registering duplicate patterns on a ServeMux.
func (g *Guard) Register(mux *http.ServeMux, auth Authenticate) {
	for _, pattern := range []string{"GET /api/security-audit/events", "GET /api/security-audit/events/export", "GET /api/security-audit/events/{id}", "PATCH /api/security-audit/events/{id}"} {
		mux.Handle(pattern, g.Handler(auth))
	}
}
func (g *Guard) Handler(auth Authenticate) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		a, ok := authenticateRequest(w, auth, r)
		if !ok {
			return
		}
		if !csrfSafe(w, r) {
			return
		}
		switch {
		case r.Method == "PATCH":
			g.handlePatch(w, r, a)
		case r.PathValue("id") != "":
			g.handleGet(w, r, a)
		default:
			g.handleList(w, r, a)
		}
	})
}

// authenticateRequest runs auth and writes a 401 if it fails or auth is nil.
// The bool return reports whether the handler should continue.
func authenticateRequest(w http.ResponseWriter, auth Authenticate, r *http.Request) (Actor, bool) {
	if auth == nil {
		reply(w, 401, map[string]string{"error": "authentication required"})
		return Actor{}, false
	}
	a, err := auth(r)
	if err != nil || a.UserID <= 0 {
		reply(w, 401, map[string]string{"error": "authentication required"})
		return Actor{}, false
	}
	return a, true
}

// csrfSafe rejects cross-site state-changing requests, writing a 403 if
// rejected. GET/HEAD are always safe.
func csrfSafe(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == "GET" || r.Method == "HEAD" {
		return true
	}
	origin := r.Header.Get("Origin")
	u, e := url.Parse(origin)
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (origin != "" && (e != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https"))) {
		reply(w, 403, map[string]string{"error": "cross-site changes are forbidden"})
		return false
	}
	return true
}

func (g *Guard) handlePatch(w http.ResponseWriter, r *http.Request, a Actor) {
	var in struct {
		Status       string `json:"status"`
		ReviewStatus string `json:"review_status"`
		Note         string `json:"note"`
		ReviewNote   string `json:"review_note"`
	}
	body := http.MaxBytesReader(w, r.Body, 8192)
	dec := json.NewDecoder(body)
	if dec.Decode(&in) != nil || dec.Decode(&struct{}{}) != io.EOF {
		reply(w, 400, map[string]string{"error": "invalid review body"})
		return
	}
	if in.Status == "" {
		in.Status = in.ReviewStatus
	}
	if in.Note == "" {
		in.Note = in.ReviewNote
	}
	event, e := g.Review(r.Context(), a, r.PathValue("id"), in.Status, in.Note)
	if e != nil {
		httpError(w, e)
		return
	}
	reply(w, 200, event)
}

func (g *Guard) handleGet(w http.ResponseWriter, r *http.Request, a Actor) {
	event, e := g.Get(r.Context(), a, r.PathValue("id"))
	if e != nil {
		httpError(w, e)
		return
	}
	reply(w, 200, event)
}

func (g *Guard) handleList(w http.ResponseWriter, r *http.Request, a Actor) {
	q := queryFrom(r)
	if r.URL.Path != "" && len(r.URL.Path) >= 7 && r.URL.Path[len(r.URL.Path)-7:] == "/export" {
		var out bytes.Buffer
		if e := g.ExportCSV(r.Context(), a, q, &out); e != nil {
			httpError(w, e)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="security-audit.csv"`)
		_, _ = w.Write(out.Bytes())
		return
	}
	list, e := g.List(r.Context(), a, q)
	if e != nil {
		httpError(w, e)
		return
	}
	reply(w, 200, list)
}
func queryFrom(r *http.Request) Query {
	q := r.URL.Query()
	integer := func(k string) int64 { n, _ := strconv.ParseInt(q.Get(k), 10, 64); return n }
	return Query{Page: int(integer("page")), PageSize: int(integer("page_size")), Source: q.Get("source"), ReviewStatus: q.Get("review_status"), MarketplaceChannel: q.Get("marketplace_channel"), Model: q.Get("model"), Search: q.Get("search"), StartTimestamp: integer("start_timestamp"), EndTimestamp: integer("end_timestamp")}
}
func reply(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func httpError(w http.ResponseWriter, err error) {
	status, message := 503, "audit service is temporarily unavailable"
	switch {
	case errors.Is(err, ErrNotFound):
		status, message = 404, "audit event not found"
	case errors.Is(err, ErrInvalid):
		status, message = 400, "invalid audit query"
	case errors.Is(err, ErrExportLimit):
		status, message = 400, ErrExportLimit.Error()
	}
	reply(w, status, map[string]string{"error": message})
}
