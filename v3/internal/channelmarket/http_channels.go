package channelmarket

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (s *Service) httpCreate(w http.ResponseWriter, r *http.Request, a Actor) {
	if !s.requireSupplierAgreement(w, r, a) {
		return
	}
	var input CreateRequest
	if !decode(w, r, &input) {
		return
	}
	result, err := s.Create(r.Context(), a.UserID, input)
	s.result(w, result, err)
}
func (s *Service) httpGroups(w http.ResponseWriter, r *http.Request, a Actor) {
	if r.URL.Query().Has("page") || r.URL.Query().Has("page_size") {
		options, err := parseBrowse(r)
		if err != nil {
			s.result(w, nil, err)
			return
		}
		page, err := s.BrowseGroups(r.Context(), a, options)
		if err != nil {
			s.result(w, nil, err)
			return
		}
		respondGroupPage(w, page)
		return
	}
	items, err := s.List(r.Context(), a, false)
	s.result(w, items, err)
}
func (s *Service) httpMine(w http.ResponseWriter, r *http.Request, a Actor) {
	items, err := s.List(r.Context(), a, true)
	s.result(w, items, err)
}
func (s *Service) httpGroup(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := s.Get(r.Context(), a, r.PathValue("slug"))
	s.result(w, result, err)
}
func (s *Service) httpModels(w http.ResponseWriter, r *http.Request, a Actor) {
	items, err := s.readPublicMarketGroups(r.Context(), a)
	if err != nil {
		s.result(w, nil, err)
		return
	}
	models := []string{}
	seen := map[string]bool{}
	for _, c := range items {
		for _, m := range c.Models {
			if !seen[m] {
				seen[m] = true
				models = append(models, m)
			}
		}
	}
	respond(w, models)
}
func (s *Service) httpUpdate(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	var patch json.RawMessage
	if !decode(w, r, &patch) {
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(patch, &fields) == nil {
		var visibility string
		if raw, exists := fields["visibility"]; exists && json.Unmarshal(raw, &visibility) == nil && visibility == "public" {
			previous, err := s.Get(r.Context(), a, r.PathValue("id"))
			if err != nil {
				s.result(w, nil, err)
				return
			}
			if previous.Visibility != "public" && !a.Admin && !s.requireSupplierAgreement(w, r, a) {
				return
			}
		}
	}
	c, err := s.Update(r.Context(), a, channel, patch)
	s.result(w, c, err)
}

func (s *Service) requireSupplierAgreement(w http.ResponseWriter, r *http.Request, a Actor) bool {
	accepted, err := s.SupplierAgreementAccepted(r.Context(), a.UserID)
	if err != nil {
		s.result(w, nil, err)
		return false
	}
	if !accepted {
		fail(w, http.StatusPreconditionRequired, "supplier_agreement_required", "请先阅读并同意当前渠道供给与结算协议")
		return false
	}
	return true
}
func (s *Service) httpTransition(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	action := "delete"
	if r.Method == http.MethodPost {
		parts := strings.Split(r.URL.Path, "/")
		action = parts[len(parts)-1]
	}
	s.result(w, nil, s.Transition(r.Context(), a, channel, action, ""))
}
func (s *Service) httpReview(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	var input struct {
		Approved bool   `json:"approved"`
		Action   string `json:"action"`
		Reason   string `json:"reason"`
	}
	if !decode(w, r, &input) {
		return
	}
	action := "reject"
	if input.Approved || input.Action == "approve" {
		action = "approve"
	}
	s.result(w, nil, s.Transition(r.Context(), a, channel, action, input.Reason))
}
func (s *Service) httpBind(w http.ResponseWriter, r *http.Request, a Actor) {
	var input struct {
		TokenID int64 `json:"token_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	result, err := s.BindGroup(r.Context(), a.UserID, r.PathValue("id"), input.TokenID)
	s.result(w, result, err)
}
func (s *Service) httpInvite(w http.ResponseWriter, r *http.Request, a Actor) {
	g, err := s.Get(r.Context(), a, r.PathValue("id"))
	if err != nil {
		s.result(w, nil, err)
		return
	}
	var input struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	if r.ContentLength != 0 && !decode(w, r, &input) {
		return
	}
	result, err := s.CreateInvite(r.Context(), a, g.InternalChannelID, input.ExpiresAt)
	s.result(w, result, err)
}
func (s *Service) httpAcceptInvite(w http.ResponseWriter, r *http.Request, a Actor) {
	var input struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &input) {
		return
	}
	group, err := s.AcceptInvite(r.Context(), a.UserID, input.Token)
	s.result(w, map[string]string{"group_id": group}, err)
}
func (s *Service) httpFeedback(w http.ResponseWriter, r *http.Request, a Actor) {
	s.result(w, nil, s.Feedback(r.Context(), a.UserID, r.PathValue("id")))
}
func (s *Service) httpBlocks(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	result, err := s.Blocks(r.Context(), a, channel)
	s.result(w, result, err)
}
func (s *Service) httpBlock(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	var input struct {
		UserID  int64 `json:"user_id"`
		Blocked bool  `json:"blocked"`
	}
	if !decode(w, r, &input) {
		return
	}
	s.result(w, nil, s.SetBlock(r.Context(), a, channel, input.UserID, input.Blocked))
}
func (s *Service) httpRemoveModel(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	var input struct {
		Model string `json:"model"`
	}
	if !decode(w, r, &input) {
		return
	}
	s.result(w, nil, s.RemoveFailedModel(r.Context(), a, channel, input.Model))
}
