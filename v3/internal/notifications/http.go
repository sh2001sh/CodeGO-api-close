package notifications

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/sh2001sh/new-api/v3/internal/control"
)

type Authenticate func(*http.Request) (int64, error)

func (s *Service) Register(mux *http.ServeMux, auth Authenticate) {
	bind := func(pattern string, fn func(http.ResponseWriter, *http.Request, int64)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/notifications/events" {
				// EventSource is cookie authenticated, including the initial check.
				r = r.Clone(r.Context())
				r.Header.Del("Authorization")
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if auth == nil {
				control.Fail(w, 401, "authentication_required", "请先登录")
				return
			}
			user, err := auth(r)
			if err != nil || user <= 0 {
				control.Fail(w, 401, "authentication_required", "请先登录")
				return
			}
			if r.Method != http.MethodGet && !sameOrigin(r) {
				control.Fail(w, 403, "invalid_origin", "请求来源无效")
				return
			}
			fn(w, r, user)
		})
	}
	bind("GET /api/notifications", s.httpList)
	bind("GET /api/notifications/summary", func(w http.ResponseWriter, r *http.Request, u int64) {
		n, e := s.Summary(r.Context(), u)
		s.reply(w, n, e)
	})
	bind("POST /api/notifications/{id}/read", func(w http.ResponseWriter, r *http.Request, u int64) {
		var payload struct {
			Read *bool `json:"read"`
		}
		present, ok := decodePayload(w, r, &payload, true)
		if !ok {
			return
		}
		if present && payload.Read == nil {
			s.reply(w, nil, ErrInvalid)
			return
		}
		read := true
		if payload.Read != nil {
			read = *payload.Read
		}
		s.reply(w, map[string]any{"read": read}, s.Read(r.Context(), u, r.PathValue("id"), read))
	})
	bind("POST /api/notifications/read-all", func(w http.ResponseWriter, r *http.Request, u int64) {
		var payload struct {
			ThroughID string `json:"through_id"`
			Category  string `json:"category"`
		}
		if _, ok := decodePayload(w, r, &payload, false); !ok {
			return
		}
		queryCategory := r.URL.Query().Get("category")
		if payload.Category != "" && queryCategory != "" && normalizeCategory(payload.Category) != normalizeCategory(queryCategory) {
			s.reply(w, nil, ErrInvalid)
			return
		}
		if payload.Category == "" {
			payload.Category = queryCategory
		}
		s.reply(w, map[string]any{"read": true}, s.ReadAll(r.Context(), u, payload.Category, payload.ThroughID))
	})
	bind("GET /api/notifications/events", func(w http.ResponseWriter, r *http.Request, u int64) { s.httpEvents(w, r, u, auth) })
}

func decodePayload(w http.ResponseWriter, r *http.Request, dst any, optional bool) (bool, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	err := d.Decode(dst)
	if optional && errors.Is(err, io.EOF) {
		return false, true
	}
	if err != nil {
		control.Fail(w, 400, "invalid_payload", "请求内容无效")
		return false, false
	}
	if err = d.Decode(new(any)); !errors.Is(err, io.EOF) {
		control.Fail(w, 400, "invalid_payload", "仅允许一份请求内容")
		return false, false
	}
	return true, true
}
func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if raw := r.Header.Get("Origin"); raw != "" {
		u, e := url.Parse(raw)
		return e == nil && u.User == nil && u.Host == r.Host && (u.Scheme == "https" || u.Scheme == "http")
	}
	return true
}
func (s *Service) reply(w http.ResponseWriter, v any, err error) {
	if err == nil {
		control.Reply(w, 200, v)
		return
	}
	switch {
	case errors.Is(err, ErrInvalid):
		control.Fail(w, 400, "invalid_value", "参数无效")
	case errors.Is(err, ErrNotFound):
		control.Fail(w, 404, "not_found", "消息不存在或无权访问")
	case errors.Is(err, errTooManyStreams):
		control.Fail(w, 429, "too_many_streams", "打开的通知连接过多")
	default:
		s.log.Error("notification operation failed", "err", err)
		control.Fail(w, 503, "unavailable", "消息服务暂时不可用")
	}
}
func (s *Service) httpList(w http.ResponseWriter, r *http.Request, u int64) {
	f := Filter{Category: r.URL.Query().Get("category"), Page: 1, PageSize: 20}
	for name, dst := range map[string]*int{"page": &f.Page, "page_size": &f.PageSize} {
		if raw := r.URL.Query().Get(name); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil {
				s.reply(w, nil, ErrInvalid)
				return
			}
			*dst = n
		}
	}
	if raw := r.URL.Query().Get("unread"); raw != "" {
		if raw != "true" && raw != "false" {
			s.reply(w, nil, ErrInvalid)
			return
		}
		f.Unread = raw == "true"
	}
	v, e := s.List(r.Context(), u, f)
	s.reply(w, v, e)
}
func (s *Service) httpEvents(w http.ResponseWriter, r *http.Request, u int64, auth Authenticate) {
	// The stream explicitly uses the session cookie. It never accepts a token
	// in the URL, and a Bearer credential alone cannot create a stream.
	if cookie, e := r.Cookie("codego_session"); e != nil || cookie.Value == "" {
		control.Fail(w, 401, "authentication_required", "请先登录")
		return
	}
	if !sameOrigin(r) {
		control.Fail(w, 403, "invalid_origin", "请求来源无效")
		return
	}
	events, unsubscribe, e := s.hub.subscribe(r.Context(), u)
	if e != nil {
		s.reply(w, nil, e)
		return
	}
	defer unsubscribe()
	n, e := s.Summary(r.Context(), u)
	if e != nil {
		s.reply(w, nil, e)
		return
	}
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		s.reply(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	write := func(event string, summary Summary) error {
		if err := rc.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
			return err
		}
		data, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		return rc.Flush()
	}
	if write("unread_count", n) != nil {
		return
	}
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-events:
			// A transaction may emit hundreds of notifications (read-all). Fold
			// the burst into one count refresh without scheduling periodic reads.
			coalesce := time.NewTimer(150 * time.Millisecond)
			select {
			case <-r.Context().Done():
				coalesce.Stop()
				return
			case <-coalesce.C:
			}
			select {
			case <-events:
			default:
			}
			user, err := auth(r)
			if err != nil || user != u {
				return
			}
			n, err := s.Summary(r.Context(), u)
			if err != nil {
				s.log.Error("notification stream refresh failed", "err", err)
				return
			}
			if write("invalidate", n) != nil {
				return
			}
		case <-heartbeat.C:
			user, err := auth(r)
			if err != nil || user != u {
				return
			}
			if err = rc.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return
			}
			if _, err = fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			if rc.Flush() != nil {
				return
			}
		}
	}
}
