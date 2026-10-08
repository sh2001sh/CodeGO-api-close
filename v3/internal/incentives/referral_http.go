package incentives

import "net/http"

func (s *Service) registerReferralHTTP(bind func(string, bool, func(http.ResponseWriter, *http.Request, Actor))) {
	bind("GET /api/user/aff/consumption-rewards", false, func(w http.ResponseWriter, r *http.Request, a Actor) {
		v, e := s.ReferralRewards(r.Context(), a.UserID)
		reply(w, v, e)
	})
	root := func(next func(http.ResponseWriter, *http.Request, Actor)) func(http.ResponseWriter, *http.Request, Actor) {
		return func(w http.ResponseWriter, r *http.Request, a Actor) {
			if a.Role != "root" {
				reply(w, nil, errForbidden)
				return
			}
			next(w, r, a)
		}
	}
	bind("GET /api/subscription/admin/referral-policy", true, root(func(w http.ResponseWriter, r *http.Request, _ Actor) {
		v, e := s.ReferralPolicy(r.Context())
		reply(w, v, e)
	}))
	bind("PUT /api/subscription/admin/referral-policy", true, root(func(w http.ResponseWriter, r *http.Request, _ Actor) {
		var p ReferralPolicy
		if e := decodeBody(w, r, &p); e != nil {
			reply(w, nil, ErrInvalid)
			return
		}
		v, e := s.UpdateReferralPolicy(r.Context(), p)
		reply(w, v, e)
	}))
	bind("GET /api/subscription/admin/referral-qualifications", true, root(func(w http.ResponseWriter, r *http.Request, _ Actor) {
		p, n, e := paging(r)
		if e != nil {
			reply(w, nil, e)
			return
		}
		v, e := s.ReferralQualifications(r.Context(), p, n)
		reply(w, v, e)
	}))
	bind("POST /api/subscription/admin/referral-qualifications/{id}/approve", true, root(func(w http.ResponseWriter, r *http.Request, _ Actor) {
		id, e := pathID(r)
		if e != nil {
			reply(w, nil, e)
			return
		}
		reply(w, nil, s.ApproveReferralReview(r.Context(), id))
	}))
}
