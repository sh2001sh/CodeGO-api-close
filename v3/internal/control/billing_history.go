package control

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
)

func (b *Billing) history(w http.ResponseWriter, r *http.Request) {
	principal, ok := r.Context().Value(principalContextKey{}).(Principal)
	if !ok || principal.UserID <= 0 {
		Fail(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			Fail(w, 400, "invalid_limit", "每页数量必须在 1 至 200 之间")
			return
		}
		limit = parsed
	}
	page, err := ledger.ReadHistory(r.Context(), b.pool, principal.UserID,
		r.URL.Query().Get("source_account_id"), r.URL.Query().Get("before"), limit)
	if errors.Is(err, ledger.ErrHistoryQuery) {
		Fail(w, 400, "invalid_cursor", "分页位置或账户无效")
		return
	}
	if err != nil {
		b.fail(w, err)
		return
	}
	Reply(w, 200, page)
}
