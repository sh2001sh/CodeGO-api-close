package community

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/community/v1/sellers", s.auth(http.HandlerFunc(s.sellersHTTP)))
	mux.Handle("GET /api/community/v1/members/{sub}", s.auth(http.HandlerFunc(s.memberHTTP)))
	mux.Handle("GET /api/community/v1/members/{sub}/channels", s.auth(http.HandlerFunc(s.channelsHTTP)))
	mux.Handle("PUT /api/community/v1/channels/{id}/rating", s.auth(http.HandlerFunc(s.ratingHTTP)))
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	return mux
}

func (s *Service) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		value := r.Header.Get("Authorization")
		candidate := ""
		if strings.HasPrefix(value, "Bearer ") {
			candidate = strings.TrimPrefix(value, "Bearer ")
		}
		if err := s.Authorize(candidate); err != nil {
			if errors.Is(err, ErrUnauthorized) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="codego-community"`)
			}
			bridgeError(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func pagination(r *http.Request) (int, int, error) {
	p, n := 1, 20
	for key, dst := range map[string]*int{"page": &p, "page_size": &n} {
		if value := r.URL.Query().Get(key); value != "" {
			number, err := strconv.Atoi(value)
			if err != nil || number < 1 {
				return 0, 0, ErrInvalidPagination
			}
			*dst = number
		}
	}
	if p > 10000 || n > 50 {
		return 0, 0, ErrInvalidPagination
	}
	return p, n, nil
}

func (s *Service) memberHTTP(w http.ResponseWriter, r *http.Request) {
	result, err := s.GetMember(r.Context(), r.PathValue("sub"))
	bridgeResult(w, result, err)
}

func (s *Service) channelsHTTP(w http.ResponseWriter, r *http.Request) {
	p, n, err := pagination(r)
	if err != nil {
		bridgeError(w, err)
		return
	}
	v := r.URL.Query()
	result, err := s.ListChannels(r.Context(), r.PathValue("sub"), ChannelQuery{
		Page: p, PageSize: n, Keyword: v.Get("keyword"), Sort: v.Get("sort"), ViewerSubject: v.Get("viewer_sub")})
	bridgeResult(w, result, err)
}

func (s *Service) sellersHTTP(w http.ResponseWriter, r *http.Request) {
	p, n, err := pagination(r)
	if err != nil {
		bridgeError(w, err)
		return
	}
	v := r.URL.Query()
	result, err := s.ListSellers(r.Context(), SellerQuery{Page: p, PageSize: n, Keyword: v.Get("keyword"), Provider: v.Get("provider"), Sort: v.Get("sort")})
	bridgeResult(w, result, err)
}

func (s *Service) ratingHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer func() { _ = r.Body.Close() }()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var request RatingRequest
	if err := d.Decode(&request); err != nil {
		bridgeError(w, ErrInvalidRating)
		return
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		bridgeError(w, ErrInvalidRating)
		return
	}
	result, err := s.RateChannel(r.Context(), r.PathValue("id"), request)
	bridgeResult(w, result, err)
}

func bridgeResult(w http.ResponseWriter, data any, err error) {
	if err != nil {
		bridgeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Success bool `json:"success"`
		Data    any  `json:"data"`
	}{true, data})
}

func bridgeError(w http.ResponseWriter, err error) {
	status, code, message := 503, "COMMUNITY_UNAVAILABLE", "Community storage is unavailable"
	switch {
	case errors.Is(err, ErrDisabled):
		status, code, message = 503, "COMMUNITY_API_DISABLED", "Community integration is not configured"
	case errors.Is(err, ErrUnauthorized):
		status, code, message = 401, "UNAUTHORIZED", "Community service authentication failed"
	case errors.Is(err, ErrInvalidSubject):
		status, code, message = 400, "INVALID_SUBJECT", "Member subject is invalid"
	case errors.Is(err, ErrInvalidPagination):
		status, code, message = 400, "INVALID_PAGINATION", "Page must be positive and page_size must be between 1 and 50"
	case errors.Is(err, ErrInvalidQuery):
		status, code, message = 400, "INVALID_QUERY", "Community query is invalid"
	case errors.Is(err, ErrInvalidRating):
		status, code, message = 400, "INVALID_RATING", "Stars must be an integer between 1 and 5"
	case errors.Is(err, ErrMemberNotFound):
		status, code, message = 404, "MEMBER_NOT_FOUND", "Community member was not found"
	case errors.Is(err, ErrChannelNotFound):
		status, code, message = 404, "CHANNEL_NOT_FOUND", "Community channel was not found"
	case errors.Is(err, ErrInactive):
		status, code, message = 403, "MEMBER_INACTIVE", "Community member is inactive"
	case errors.Is(err, ErrSelfRating):
		status, code, message = 403, "SELF_RATING_FORBIDDEN", "Channel owners cannot rate their own channels"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}{false, code, message})
}
