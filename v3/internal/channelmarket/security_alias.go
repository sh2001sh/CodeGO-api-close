package channelmarket

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Existing route protection runs first, including the admin-only gate and
// cross-site mutation check. Delegation preserves the original string path ID.
func (s *Service) delegateSecurityAudit(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.SecurityAuditHandler == nil {
		return false
	}
	request := r.Clone(r.Context())
	query := request.URL.Query()
	if channel := query.Get("channel_id"); channel != "" {
		query.Set("marketplace_channel", channel)
	}
	for name, fallback := range map[string]string{"page": "1", "page_size": "20"} {
		if value, err := strconv.Atoi(query.Get(name)); err != nil || value <= 0 {
			query.Set(name, fallback)
		}
	}
	request.URL.RawQuery = query.Encode()
	response := &auditAliasResponse{header: make(http.Header)}
	s.cfg.SecurityAuditHandler.ServeHTTP(response, request)
	for name, values := range response.header {
		if !strings.EqualFold(name, "Content-Length") {
			w.Header()[name] = append([]string(nil), values...)
		}
	}
	status := response.status
	if status == 0 {
		status = http.StatusOK
	}
	if strings.HasPrefix(response.header.Get("Content-Type"), "text/csv") && status >= 200 && status < 300 {
		// Retain the original alias's spreadsheet formula protection even when
		// the canonical audit handler exports raw evidence cells.
		payload, err := safeAuditCSV(response.body.Bytes())
		if err != nil {
			w.Header().Del("Content-Disposition")
			fail(w, 503, "audit_unavailable", "安全审计导出无效")
			return true
		}
		w.WriteHeader(status)
		if _, err := w.Write(payload); err != nil {
			s.log.WarnContext(r.Context(), "security audit export disconnected", "err", err)
		}
		return true
	}
	if !json.Valid(response.body.Bytes()) {
		fail(w, 503, "audit_unavailable", "安全审计暂时不可用")
		return true
	}
	if status >= 200 && status < 300 {
		w.WriteHeader(status)
		respond(w, json.RawMessage(response.body.Bytes()))
		return true
	}
	var failure struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(response.body.Bytes(), &failure) != nil || failure.Error == "" {
		failure.Error = "安全审计请求失败"
	}
	fail(w, status, "audit_error", failure.Error)
	return true
}

func safeAuditCSV(raw []byte) ([]byte, error) {
	rows, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	writer := csv.NewWriter(&out)
	for _, row := range rows {
		for index := range row {
			row[index] = csvText(row[index])
		}
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	return out.Bytes(), writer.Error()
}

type auditAliasResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (w *auditAliasResponse) Header() http.Header { return w.header }
func (w *auditAliasResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *auditAliasResponse) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}
