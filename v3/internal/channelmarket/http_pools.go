package channelmarket

import (
	"net/http"
)

func (s *Service) httpPools(w http.ResponseWriter, r *http.Request, a Actor) {
	pools, err := s.Pools(r.Context(), a.UserID)
	s.result(w, pools, err)
}
func (s *Service) httpPool(w http.ResponseWriter, r *http.Request, a Actor) {
	pools, err := s.Pools(r.Context(), a.UserID)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	for _, p := range pools {
		if p.ID == r.PathValue("id") {
			respond(w, p)
			return
		}
	}
	s.result(w, nil, ErrNotFound)
}
func (s *Service) httpSavePool(w http.ResponseWriter, r *http.Request, a Actor) {
	var input RoutePool
	if !decode(w, r, &input) {
		return
	}
	if r.Method == http.MethodPut {
		input.ID = r.PathValue("id")
	}
	result, err := s.SavePool(r.Context(), a.UserID, input)
	s.result(w, result, err)
}
func (s *Service) httpDeletePool(w http.ResponseWriter, r *http.Request, a Actor) {
	s.result(w, nil, s.DeletePool(r.Context(), a.UserID, r.PathValue("id")))
}
func (s *Service) httpBindPool(w http.ResponseWriter, r *http.Request, a Actor) {
	var input struct {
		TokenID int64 `json:"token_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	result, err := s.BindRoutePool(r.Context(), a.UserID, r.PathValue("id"), input.TokenID)
	s.result(w, result, err)
}
func (s *Service) httpAutoPool(w http.ResponseWriter, r *http.Request, a Actor) {
	pools, err := s.Pools(r.Context(), a.UserID)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	for _, p := range pools {
		if p.Name == "Auto" {
			respond(w, p)
			return
		}
	}
	respond(w, RoutePool{Name: "Auto", Strategy: "priority", MaxAttempts: 3, Members: []PoolMember{}})
}
func (s *Service) httpSaveAutoPool(w http.ResponseWriter, r *http.Request, a Actor) {
	var input RoutePool
	if !decode(w, r, &input) {
		return
	}
	pools, err := s.Pools(r.Context(), a.UserID)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	for _, p := range pools {
		if p.Name == "Auto" {
			input.ID = p.ID
			break
		}
	}
	input.Name = "Auto"
	result, err := s.SavePool(r.Context(), a.UserID, input)
	s.result(w, result, err)
}
func (s *Service) httpIncome(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := s.Income(r.Context(), a)
	s.result(w, result, err)
}
func (s *Service) httpBuildPool(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := s.BuildPool(r.Context(), a.UserID, r.PathValue("id"))
	s.result(w, result, err)
}
func (s *Service) httpReclaim(w http.ResponseWriter, r *http.Request, a Actor) {
	var input ReclaimRequest
	if !decode(w, r, &input) {
		return
	}
	result, err := s.QueueReclaim(r.Context(), a, input)
	s.result(w, result, err)
}
func (s *Service) httpReclaimStatus(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := s.GetReclaim(r.Context(), a, r.PathValue("id"))
	s.result(w, result, err)
}
