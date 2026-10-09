package control

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/api"
	"github.com/sh2001sh/new-api/v3/internal/billing"
	"github.com/sh2001sh/new-api/v3/internal/billing/ledger"
	"github.com/sh2001sh/new-api/v3/pkg/credits"
)

type Billing struct {
	server   *Server
	pool     *pgxpool.Pool
	archive  *pgxpool.Pool
	accounts *ledger.Accounts
	poster   billing.Poster
}

func (s *Server) RegisterBilling(pool *pgxpool.Pool, accounts *ledger.Accounts, poster billing.Poster, archive *pgxpool.Pool) {
	b := &Billing{server: s, pool: pool, accounts: accounts, poster: poster, archive: archive}
	s.mux.Handle("GET /api/billing/balance", s.RequireUser(http.HandlerFunc(b.balance)))
	s.mux.Handle("GET /api/wallet", s.RequireUser(http.HandlerFunc(b.balance)))
	s.mux.Handle("GET /api/billing/entries", s.RequireUser(http.HandlerFunc(b.entries)))
	s.mux.Handle("GET /api/billing/history", s.RequireUser(http.HandlerFunc(b.history)))
	s.mux.Handle("GET /api/billing/funding-economics", s.RequireUser(http.HandlerFunc(b.fundingEconomics)))
	s.mux.Handle("POST /api/billing/adjustments", s.RequireAdmin(http.HandlerFunc(b.adjust)))
}

func (b *Billing) wallet(w http.ResponseWriter, r *http.Request) (int64, bool) {
	p, ok := r.Context().Value(principalContextKey{}).(Principal)
	if !ok || p.UserID <= 0 {
		Fail(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return 0, false
	}
	accountID, err := b.accounts.WalletAccount(r.Context(), p.UserID)
	if err != nil {
		b.fail(w, err)
		return 0, false
	}
	return accountID, true
}

func (b *Billing) balance(w http.ResponseWriter, r *http.Request) {
	accountID, ok := b.wallet(w, r)
	if !ok {
		return
	}
	balance, version, err := b.accounts.LedgerBalance(r.Context(), accountID)
	if err != nil {
		b.fail(w, err)
		return
	}
	Reply(w, http.StatusOK, api.Balance{AccountId: accountID, BalanceMicro: int64(balance), Version: version,
		BalanceMicroCredits: strconv.FormatInt(int64(balance), 10)})
}

type LedgerEntry struct {
	ID           int64         `json:"id"`
	Amount       credits.Micro `json:"amount_micro"`
	BalanceAfter credits.Micro `json:"balance_after_micro"`
	Kind         string        `json:"kind"`
	OperationID  string        `json:"operation_id"`
	Reason       string        `json:"reason"`
	CreatedAt    time.Time     `json:"created_at"`
}

func (b *Billing) entries(w http.ResponseWriter, r *http.Request) {
	accountID, ok := b.wallet(w, r)
	if !ok {
		return
	}
	limit, before := 50, int64(0)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 || value > 200 {
			Fail(w, 400, "invalid_limit", "每页数量必须在 1 至 200 之间")
			return
		}
		limit = value
	}
	if raw := r.URL.Query().Get("before"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			Fail(w, 400, "invalid_cursor", "分页位置无效")
			return
		}
		before = value
	}
	rows, err := b.pool.Query(r.Context(), `SELECT id, amount, balance_after, kind, operation_id, reason, created_at
		FROM v3_billing.ledger_entries WHERE account_id = $1 AND ($2::bigint = 0 OR id < $2)
		ORDER BY id DESC LIMIT $3`, accountID, before, limit+1)
	if err != nil {
		b.fail(w, err)
		return
	}
	defer rows.Close()
	items := make([]LedgerEntry, 0, limit)
	for rows.Next() {
		var e LedgerEntry
		if err := rows.Scan(&e.ID, &e.Amount, &e.BalanceAfter, &e.Kind, &e.OperationID, &e.Reason, &e.CreatedAt); err != nil {
			b.fail(w, err)
			return
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		b.fail(w, err)
		return
	}
	next := int64(0)
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	Reply(w, 200, struct {
		Items  []LedgerEntry `json:"items"`
		Before int64         `json:"next_before,omitempty"`
	}{Items: items, Before: next})
}

func (b *Billing) adjust(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AccountID   int64         `json:"account_id"`
		Amount      credits.Micro `json:"amount_micro"`
		OperationID string        `json:"operation_id"`
		Reason      string        `json:"reason"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || !errors.Is(d.Decode(new(any)), io.EOF) || input.AccountID <= 0 ||
		input.Amount == 0 || strings.TrimSpace(input.OperationID) == "" || len(input.OperationID) > 128 ||
		strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 1000 {
		Fail(w, 400, "invalid_adjustment", "需要有效的账户、非零金额、唯一操作编号和原因")
		return
	}
	result, err := b.poster.Post(r.Context(), billing.Entry{
		AccountID: input.AccountID, Amount: input.Amount, Kind: "adjustment",
		OperationID: "admin-adjustment:" + input.OperationID, Reason: input.Reason,
		Metadata: map[string]any{"source": "control", "actor_user_id": r.Context().Value(principalContextKey{}).(Principal).UserID},
	})
	if errors.Is(err, billing.ErrPostConflict) {
		Fail(w, 409, "operation_conflict", "操作编号已被不同的调整使用")
		return
	}
	if errors.Is(err, ledger.ErrUnknownAccount) {
		Fail(w, 404, "not_found", "账户不存在")
		return
	}
	if err != nil {
		b.fail(w, err)
		return
	}
	Reply(w, 200, map[string]any{"entry_id": result.EntryID, "balance_micro": result.Balance,
		"version": result.Version, "duplicate": result.Duplicate})
}

func (b *Billing) fail(w http.ResponseWriter, err error) {
	b.server.log.Error("control billing request failed", "err", err)
	Fail(w, 503, "billing_unavailable", "账本服务暂时不可用")
}
