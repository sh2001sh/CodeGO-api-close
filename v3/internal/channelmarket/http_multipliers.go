package channelmarket

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func (s *Service) httpMultiplier(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	var input struct {
		UserID     int64        `json:"user_id"`
		Multiplier *json.Number `json:"multiplier"`
	}
	if !decode(w, r, &input) {
		return
	}
	s.result(w, nil, s.SetMultiplier(r.Context(), a, channel, input.UserID, input.Multiplier))
}
func (s *Service) httpMultipliers(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := s.Multipliers(r.Context(), a)
	s.result(w, result, err)
}
func (s *Service) httpBatchMultiplier(w http.ResponseWriter, r *http.Request, a Actor) {
	var input struct {
		Targets    []MultiplierTarget `json:"targets"`
		Multiplier *json.Number       `json:"multiplier"`
		Action     string             `json:"action"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Action != "" && ((input.Action != "set" && input.Action != "clear") || (input.Action == "set" && input.Multiplier == nil) || (input.Action == "clear" && input.Multiplier != nil)) {
		s.result(w, nil, ErrInvalid)
		return
	}
	count, err := s.BatchMultipliers(r.Context(), a, input.Targets, input.Multiplier)
	s.result(w, map[string]int{"changed": count, "changed_count": count}, err)
}
func (s *Service) httpNotices(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := s.Notices(r.Context(), a.UserID)
	s.result(w, result, err)
}
func (s *Service) httpReadNotice(w http.ResponseWriter, r *http.Request, a Actor) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.result(w, nil, ErrInvalid)
		return
	}
	s.result(w, nil, s.ReadNotice(r.Context(), a.UserID, id))
}
func (s *Service) httpTimeMultipliers(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	result, err := s.TimeMultipliers(r.Context(), a, channel)
	s.result(w, result, err)
}
func (s *Service) httpSaveTimeMultiplier(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	var input TimeMultiplier
	if !decode(w, r, &input) {
		return
	}
	input.ChannelID = channel
	result, err := s.SaveTimeMultiplier(r.Context(), a, input)
	s.result(w, result, err)
}
func (s *Service) httpDeleteTimeMultiplier(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	s.result(w, nil, s.DeleteTimeMultiplier(r.Context(), a, channel, r.PathValue("ruleId")))
}
func (s *Service) httpBargain(w http.ResponseWriter, r *http.Request, a Actor) {
	var input Bargain
	if !decode(w, r, &input) {
		return
	}
	input.GroupID = r.PathValue("id")
	result, err := s.RequestBargain(r.Context(), a.UserID, input)
	s.result(w, result, err)
}
func (s *Service) httpBargains(w http.ResponseWriter, r *http.Request, a Actor) {
	result, err := s.Bargains(r.Context(), a)
	s.result(w, result, err)
}
func (s *Service) httpResolveBargain(w http.ResponseWriter, r *http.Request, a Actor) {
	var input struct {
		Accept bool   `json:"accept"`
		Note   string `json:"note"`
	}
	if !decode(w, r, &input) {
		return
	}
	s.result(w, nil, s.ResolveBargain(r.Context(), a, r.PathValue("id"), input.Accept, input.Note))
}
