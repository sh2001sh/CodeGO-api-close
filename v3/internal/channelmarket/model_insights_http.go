package channelmarket

import (
	"net/http"
	"strconv"
)

func (s *Service) RegisterInsightsHTTP(mux *http.ServeMux, auth Authenticate) {
	mux.Handle("GET /api/marketplace/groups/{id}/insights", s.protect(auth, true, false, s.httpModelInsights))
	mux.Handle("GET /api/marketplace/channels/{id}/disclosure", s.protect(auth, false, false, s.httpDisclosure))
	mux.Handle("PUT /api/marketplace/channels/{id}/disclosure", s.protect(auth, false, false, s.httpSaveDisclosure))
}

func (s *Service) httpModelInsights(w http.ResponseWriter, r *http.Request, a Actor) {
	hours := 24
	if value := r.URL.Query().Get("window_hours"); value != "" {
		var err error
		hours, err = strconv.Atoi(value)
		if err != nil || (hours != 24 && hours != 168) {
			s.result(w, nil, ErrInvalid)
			return
		}
	}
	result, err := s.ModelInsights(r.Context(), a, r.PathValue("id"), r.URL.Query().Get("model"), hours)
	s.result(w, result, err)
}

func (s *Service) httpDisclosure(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	result, err := s.Disclosure(r.Context(), a, channel)
	s.result(w, result, err)
}

func (s *Service) httpSaveDisclosure(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	var input ChannelMarketDisclosureInput
	if !decode(w, r, &input) {
		return
	}
	result, err := s.SaveDisclosure(r.Context(), a, channel, input)
	s.result(w, result, err)
}
