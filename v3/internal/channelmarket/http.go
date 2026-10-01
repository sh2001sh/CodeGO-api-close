package channelmarket

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5/pgconn"
)

type Authenticate func(*http.Request) (Actor, error)
type endpoint func(http.ResponseWriter, *http.Request, Actor)

func (s *Service) Register(mux *http.ServeMux, auth Authenticate) {
	public := map[string]endpoint{
		"GET /api/marketplace/groups": s.httpGroups, "GET /api/marketplace/models": s.httpModels,
		"GET /api/marketplace/group-status": s.httpGroups, "GET /api/marketplace/groups/{slug}": s.httpGroup,
		"GET /api/marketplace/groups/{slug}/model-status": s.httpGroup, "GET /api/marketplace/multiplier-trends": s.httpTrends,
	}
	private := map[string]endpoint{
		"GET /api/marketplace/channels/{id}/user-usage/{userId}/time-series": s.httpUsageSeries,
		"GET /api/marketplace/security-audit/events/export":                  s.httpExportSecurity,
		"POST /api/marketplace/channels/{id}/batch-welfare":                  s.httpWelfare,
		"POST /api/marketplace/channels/fetch-models":                        s.httpFetchModels,
		"POST /api/marketplace/batch-tests":                                  s.httpStartBatch, "GET /api/marketplace/batch-tests/{id}": s.httpBatch,
		"POST /api/marketplace/channels": s.httpCreate, "GET /api/marketplace/channels/mine": s.httpMine,
		"PATCH /api/marketplace/channels/{id}": s.httpUpdate, "DELETE /api/marketplace/channels/{id}": s.httpTransition,
		"POST /api/marketplace/channels/{id}/pause": s.httpTransition, "POST /api/marketplace/channels/{id}/resume": s.httpTransition,
		"POST /api/marketplace/channels/{id}/verify": s.httpVerify, "POST /api/marketplace/channels/{id}/test": s.httpVerify,
		"POST /api/marketplace/channels/{id}/test/failed": s.httpVerify, "POST /api/marketplace/channels/{id}/verification/pause": s.httpPauseVerify,
		"POST /api/marketplace/channels/{id}/models/remove-failed": s.httpRemoveModel,
		"GET /api/marketplace/key-group-options":                   s.httpGroups, "POST /api/marketplace/groups/{id}/bind-token": s.httpBind,
		"POST /api/marketplace/groups/{id}/invite": s.httpInvite, "POST /api/marketplace/invites/accept": s.httpAcceptInvite,
		"POST /api/marketplace/groups/{id}/feedback":     s.httpFeedback,
		"GET /api/marketplace/channels/{id}/user-blocks": s.httpBlocks, "POST /api/marketplace/channels/{id}/user-block": s.httpBlock,
		"POST /api/marketplace/channels/{id}/user-multiplier": s.httpMultiplier, "GET /api/marketplace/channels/mine/user-multipliers": s.httpMultipliers,
		"POST /api/marketplace/channels/mine/user-multipliers/batch": s.httpBatchMultiplier,
		"GET /api/marketplace/multiplier-notices":                    s.httpNotices, "POST /api/marketplace/multiplier-notices/{id}/read": s.httpReadNotice,
		"GET /api/marketplace/channels/{id}/time-range-multipliers": s.httpTimeMultipliers, "POST /api/marketplace/channels/{id}/time-range-multipliers": s.httpSaveTimeMultiplier, "DELETE /api/marketplace/channels/{id}/time-range-multipliers/{ruleId}": s.httpDeleteTimeMultiplier,
		"POST /api/marketplace/groups/{id}/bargain-requests": s.httpBargain, "GET /api/marketplace/channels/mine/bargain-requests": s.httpBargains, "POST /api/marketplace/channels/mine/bargain-requests/{id}/resolve": s.httpResolveBargain,
		"GET /api/marketplace/route-pools": s.httpPools, "POST /api/marketplace/route-pools": s.httpSavePool, "GET /api/marketplace/route-pools/{id}": s.httpPool, "PUT /api/marketplace/route-pools/{id}": s.httpSavePool, "DELETE /api/marketplace/route-pools/{id}": s.httpDeletePool, "POST /api/marketplace/route-pools/{id}/bind-token": s.httpBindPool,
		"GET /api/marketplace/auto-route-pool": s.httpAutoPool, "PUT /api/marketplace/auto-route-pool": s.httpSaveAutoPool,
		"POST /api/marketplace/route-pools/{id}/auto-build/run": s.httpBuildPool,
		"GET /api/marketplace/channels/mine/logs":               s.httpLogs, "GET /api/marketplace/channels/mine/logs/export": s.httpExportLogs,
		"GET /api/marketplace/channels/mine/observability": s.httpIncome, "GET /api/marketplace/channels/mine/user-usage": s.httpUsage,
		"GET /api/marketplace/security-audit/events": s.httpSecurity, "PATCH /api/marketplace/security-audit/events/{id}": s.httpResolveSecurity,
	}
	admin := map[string]endpoint{
		"GET /api/marketplace/admin/security-audit/events/export": s.httpExportSecurity,
		"POST /api/marketplace/admin/channels/{id}/test/failed":   s.httpVerify, "POST /api/marketplace/admin/channels/{id}/models/remove-failed": s.httpRemoveModel, "POST /api/marketplace/admin/channels/{id}/verification/pause": s.httpPauseVerify,
		"GET /api/marketplace/admin/channels": s.httpMine, "PATCH /api/marketplace/admin/channels/{id}": s.httpUpdate, "DELETE /api/marketplace/admin/channels/{id}": s.httpTransition,
		"POST /api/marketplace/admin/channels/{id}/review": s.httpReview, "POST /api/marketplace/admin/channels/{id}/verify": s.httpVerify, "POST /api/marketplace/admin/channels/{id}/test": s.httpVerify, "POST /api/marketplace/admin/channels/{id}/pause": s.httpTransition, "POST /api/marketplace/admin/channels/{id}/resume": s.httpTransition,
		"GET /api/marketplace/admin/owner-income": s.httpIncome, "POST /api/marketplace/admin/owner-income/release": s.httpReclaim, "GET /api/marketplace/admin/owner-income/reclaims/{id}": s.httpReclaimStatus,
		"GET /api/marketplace/admin/security-audit/events": s.httpSecurity, "PATCH /api/marketplace/admin/security-audit/events/{id}": s.httpResolveSecurity,
	}
	for pattern, fn := range public {
		mux.Handle(pattern, s.protect(auth, true, false, fn))
	}
	for pattern, fn := range private {
		mux.Handle(pattern, s.protect(auth, false, false, fn))
	}
	for pattern, fn := range admin {
		mux.Handle(pattern, s.protect(auth, false, true, fn))
	}
}

func (s *Service) protect(auth Authenticate, public, admin bool, fn endpoint) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		var a Actor
		if auth != nil {
			var err error
			a, err = auth(r)
			if err != nil && !public {
				fail(w, 401, "unauthorized", "请先登录")
				return
			}
		}
		if (!public && a.UserID <= 0) || (admin && !a.Admin) {
			fail(w, 403, "forbidden", "无权执行此操作")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host || (u.Scheme != "https" && u.Scheme != "http") {
					fail(w, 403, "forbidden", "拒绝跨站变更")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "forbidden", "拒绝跨站变更")
				return
			}
		}
		fn(w, r, a)
	})
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.UseNumber()
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		fail(w, 400, "invalid_payload", "请求内容无效")
		return false
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		fail(w, 400, "invalid_payload", "仅允许一份请求内容")
		return false
	}
	return true
}
func respond(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "", "data": data})
}
func fail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "code": code, "message": message})
}
func (s *Service) result(w http.ResponseWriter, data any, err error) {
	if err == nil {
		respond(w, data)
		return
	}
	var p *pgconn.PgError
	switch {
	case errors.Is(err, ErrInvalid):
		fail(w, 400, "invalid_value", "参数无效")
	case errors.Is(err, ErrNotFound):
		fail(w, 404, "not_found", "资源不存在或无权访问")
	case errors.Is(err, ErrConflict), errors.As(err, &p) && (p.Code == "23505" || p.Code == "23503"):
		fail(w, 409, "conflict", "资源或状态冲突")
	case errors.As(err, &p) && (p.Code == "23514" || p.Code == "22P02"):
		fail(w, 400, "invalid_value", "参数无效")
	default:
		s.log.Error("channel market operation failed", "err", err)
		fail(w, 503, "market_unavailable", "渠道市场暂时不可用")
	}
}
func (s *Service) httpChannel(w http.ResponseWriter, r *http.Request, a Actor) (int64, bool) {
	id, err := s.ChannelID(r.Context(), a, r.PathValue("id"))
	if err != nil {
		s.result(w, nil, err)
		return 0, false
	}
	return id, true
}
func integer(r *http.Request, key string, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || n <= 0 {
		return def
	}
	if n > 1000 {
		return 1000
	}
	return n
}
