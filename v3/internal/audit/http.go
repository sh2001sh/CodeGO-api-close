package audit

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RegisterRoutes keeps the existing log paths and adds bounded CSV export.
// Authentication must be supplied; a missing callback always denies access.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	for _, path := range []string{"/api/log/{$}", "/api/log/self", "/api/log/search", "/api/log/self/search", "/api/log/token", "/api/audit/usage"} {
		mux.HandleFunc("GET "+path, s.listHTTP)
	}
	for _, path := range []string{"/api/log/stat", "/api/log/self/stat", "/api/audit/usage/stat"} {
		mux.HandleFunc("GET "+path, s.summaryHTTP)
	}
	for _, path := range []string{"/api/log/export", "/api/log/self/export", "/api/audit/usage/export"} {
		mux.HandleFunc("GET "+path, s.exportHTTP)
	}
	mux.HandleFunc("GET /api/audit/samples/{request}", s.sampleHTTP)
	mux.HandleFunc("GET /api/audit/events", s.eventsHTTP)
	mux.HandleFunc("GET /api/audit/events/export", s.eventsExportHTTP)
	mux.HandleFunc("GET /api/audit/requests", s.requestsHTTP)
	mux.HandleFunc("GET /api/audit/requests/{request}/attempts", s.attemptsHTTP)
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	return mux
}

func (s *Service) principal(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if s.cfg.Authenticate == nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication is required")
		return Principal{}, false
	}
	p, err := s.cfg.Authenticate(r)
	if err != nil || p.UserID <= 0 {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication failed")
		return Principal{}, false
	}
	if r.URL.Path == "/api/log/token" && p.KeyID <= 0 {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "A read-only API key is required")
		return Principal{}, false
	}
	if strings.HasPrefix(r.URL.Path, "/api/log/") && !strings.Contains(r.URL.Path, "/self") && r.URL.Path != "/api/log/token" && (!p.Admin || p.KeyID > 0) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "Administrator authorization is required")
		return Principal{}, false
	}
	if strings.Contains(r.URL.Path, "/self") {
		p.Admin = false
	}
	return p, true
}

func parseQuery(r *http.Request) (Query, error) {
	v := r.URL.Query()
	q := Query{Model: v.Get("model"), Cursor: v.Get("cursor")}
	if q.Model == "" {
		q.Model = v.Get("model_name")
	}
	for name, dst := range map[string]*int64{"user_id": &q.UserID, "key_id": &q.KeyID, "channel_id": &q.ChannelID} {
		if value := v.Get(name); value != "" {
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				return q, ErrInvalid
			}
			*dst = n
		}
	}
	if value := v.Get("page_size"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 200 {
			return q, ErrInvalid
		}
		q.Limit = n
	}
	for _, f := range []struct {
		names []string
		dst   *time.Time
	}{
		{[]string{"from", "start_timestamp"}, &q.From}, {[]string{"to", "end_timestamp"}, &q.To},
	} {
		for _, name := range f.names {
			value := v.Get(name)
			if value == "" {
				continue
			}
			if n, err := strconv.ParseInt(value, 10, 64); err == nil {
				if n < 0 || n > 253402300799 {
					return q, ErrInvalid
				}
				*f.dst = time.Unix(n, 0).UTC()
			} else {
				t, err := time.Parse(time.RFC3339Nano, value)
				if err != nil {
					return q, ErrInvalid
				}
				*f.dst = t
			}
			break
		}
	}
	if value := v.Get("page"); value != "" && value != "1" {
		// Offset pagination can skip/duplicate rows while requests complete.
		// Follow next_cursor instead of asking for an arbitrary numbered page.
		return q, ErrInvalid
	}
	return q, nil
}

func (s *Service) listHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	q, err := parseQuery(r)
	if err != nil {
		reportError(w, err)
		return
	}
	page, err := s.List(r.Context(), p, q)
	if err != nil {
		reportError(w, err)
		return
	}
	writeData(w, page)
}

func (s *Service) summaryHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	q, err := parseQuery(r)
	if err != nil {
		reportError(w, err)
		return
	}
	result, err := s.Summarize(r.Context(), p, q)
	if err != nil {
		reportError(w, err)
		return
	}
	writeData(w, result)
}

func (s *Service) exportHTTP(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	q, err := parseQuery(r)
	if err != nil {
		reportError(w, err)
		return
	}
	// Exports are cursor pages as well. This bounds request time and memory;
	// clients follow X-Next-Cursor until absent for a complete export.
	page, err := s.List(r.Context(), p, q)
	if err != nil {
		reportError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="usage.csv"`)
	if page.HasMore {
		w.Header().Set("X-Next-Cursor", page.NextCursor)
	}
	c := csv.NewWriter(w)
	if err := c.Write([]string{"request_id", "created_at", "model", "amount_micro_credits", "prompt_tokens", "completion_tokens", "terminal"}); err != nil {
		return
	}
	for _, u := range page.Items {
		if err := c.Write([]string{safeCSV(u.RequestID), u.CreatedAt.UTC().Format(time.RFC3339Nano), safeCSV(u.Model),
			strconv.FormatInt(int64(u.Amount), 10), strconv.FormatInt(u.PromptTokens, 10), strconv.FormatInt(u.CompletionTokens, 10), safeCSV(u.Terminal)}); err != nil {
			return
		}
	}
	c.Flush()
}

// Spreadsheet applications interpret these prefixes as formulas even in CSV.
func safeCSV(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}

func reportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeError(w, 400, "INVALID_QUERY", "Invalid filters or cursor; use next_cursor to continue")
	case errors.Is(err, ErrForbidden):
		writeError(w, 403, "FORBIDDEN", "Usage belongs to another account")
	case errors.Is(err, ErrNotFound):
		writeError(w, 404, "REQUEST_NOT_FOUND", "Request history is unavailable for this account")
	default:
		writeError(w, 503, "AUDIT_UNAVAILABLE", "Usage storage is unavailable")
	}
}

func writeData(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Success bool `json:"success"`
		Data    any  `json:"data"`
	}{true, data})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}{false, code, message})
}
