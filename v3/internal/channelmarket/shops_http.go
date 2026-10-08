package channelmarket

import "net/http"

func (s *Service) httpShops(w http.ResponseWriter, r *http.Request, a Actor) {
	if r.URL.Query().Has("page") || r.URL.Query().Has("page_size") {
		options, err := parseBrowse(r)
		if err == nil && options.Sort != "recommended" && options.Sort != "name" {
			err = ErrInvalid
		}
		if err != nil {
			s.result(w, nil, err)
			return
		}
		page, err := s.BrowseShops(r.Context(), a, options)
		if err != nil {
			s.result(w, nil, err)
			return
		}
		respondShopPage(w, page)
		return
	}
	items, err := s.ListShops(r.Context(), a, false)
	s.result(w, items, err)
}
func (s *Service) httpShop(w http.ResponseWriter, r *http.Request, a Actor) {
	if r.URL.Query().Has("page") || r.URL.Query().Has("page_size") {
		options, err := parseBrowse(r)
		if err != nil {
			s.result(w, nil, err)
			return
		}
		item, err := s.BrowseShop(r.Context(), a, r.PathValue("id"), options)
		s.result(w, item, err)
		return
	}
	item, err := s.GetShop(r.Context(), a, r.PathValue("id"))
	s.result(w, item, err)
}
func (s *Service) httpMyShop(w http.ResponseWriter, r *http.Request, a Actor) {
	item, err := s.MyShop(r.Context(), a)
	s.result(w, item, err)
}
func (s *Service) httpUpdateShop(w http.ResponseWriter, r *http.Request, a Actor) {
	var input ShopUpdate
	if !decode(w, r, &input) {
		return
	}
	item, err := s.UpdateShop(r.Context(), a, input)
	s.result(w, item, err)
}
func (s *Service) httpAdminShops(w http.ResponseWriter, r *http.Request, a Actor) {
	items, err := s.ListShops(r.Context(), a, true)
	s.result(w, items, err)
}
func (s *Service) httpReviewShop(w http.ResponseWriter, r *http.Request, a Actor) {
	var input struct {
		Approved bool   `json:"approved"`
		Reason   string `json:"reason"`
	}
	if !decode(w, r, &input) {
		return
	}
	s.result(w, nil, s.ReviewShop(r.Context(), a, r.PathValue("id"), input.Approved, input.Reason))
}
