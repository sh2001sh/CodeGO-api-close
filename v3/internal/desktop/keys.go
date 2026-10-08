package desktop

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/identity"
	"github.com/sh2001sh/new-api/v3/internal/legacy/identitydto"
)

const keySelect = `SELECT k.id,k.user_id,k.name,k.status,k.group_name,k.allowed_models,k.allowed_cidrs::text[],k.expires_at,k.key_prefix,k.created_at,k.last_used_at,
	 k.budget_limited,CASE WHEN k.budget_limited THEN coalesce(b.balance,0) ELSE NULL END,k.cross_group_retry,(k.max_marketplace_multiplier*1000000)::bigint,
	 coalesce(b.id,0),coalesce((SELECT sum(l.amount) FROM v3_billing.usage_logs l WHERE l.key_id=k.id AND l.user_id=k.user_id),0)::bigint
	 FROM v3_identity.api_keys k LEFT JOIN v3_billing.accounts b ON b.owner_type='api_key' AND b.owner_id=k.id AND b.kind='key_budget'`

func scanOwnedKey(row pgx.Row) (identity.KeyRecord, error) {
	var k identity.KeyRecord
	err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.Status, &k.Group, &k.AllowedModels, &k.AllowedCIDRs, &k.ExpiresAt, &k.Prefix, &k.CreatedAt, &k.LastUsedAt,
		&k.BudgetLimited, &k.BudgetMicroCredits, &k.CrossGroupRetry, &k.MaxMarketplaceMultiplierPPM, &k.BudgetAccountID, &k.SpentMicroCredits)
	return k, dbError(err)
}

func (s *Service) loadKey(ctx context.Context, uid, kid int64) (identity.KeyRecord, error) {
	return scanOwnedKey(s.pool.QueryRow(ctx, keySelect+` WHERE k.user_id=$1 AND k.id=$2 AND k.deleted_at IS NULL`, uid, kid))
}

func (s *Service) keysHTTP(w http.ResponseWriter, r *http.Request) {
	scope := "tokens:read"
	if r.Method != "GET" {
		scope = "tokens:write"
	}
	d, ok := s.device(w, r, scope)
	if !ok {
		return
	}
	mapped := r.Clone(r.Context())
	mapped.URL.Path = "/api/token/"
	identitydto.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			if identitydto.IsV3(r) {
				keys, err := s.id.ListKeys(r.Context(), d.UserID, positiveID(r.URL.Query().Get("before")), int(positiveID(r.URL.Query().Get("page_size"))))
				reply(w, keys, err)
				return
			}
			out, err := s.keyPage(r.Context(), d.UserID, r)
			reply(w, out, err)
		case "POST", "PUT":
			var in identity.KeyRecord
			if err := decode(w, r, &in); err != nil {
				reply(w, nil, err)
				return
			}
			if r.Method == "PUT" {
				reply(w, nil, s.id.UpdateKey(r.Context(), d.UserID, in.KeyInput))
				return
			}
			k, raw, err := s.id.CreateKey(r.Context(), d.UserID, in.KeyInput)
			reply(w, struct {
				identity.KeyRecord
				Key string `json:"key"`
			}{k, raw}, err)
		case "DELETE":
			reply(w, nil, s.id.DeleteKey(r.Context(), d.UserID, positiveID(r.PathValue("id"))))
		}
	})).ServeHTTP(w, mapped)
}

func (s *Service) keyPage(ctx context.Context, uid int64, r *http.Request) (map[string]any, error) {
	page := int(positiveID(r.URL.Query().Get("p")))
	if page < 1 {
		page = 1
	}
	if page > 10000 {
		return nil, ErrInvalid
	}
	limit := int(positiveID(r.URL.Query().Get("page_size")))
	if limit == 0 {
		limit = int(positiveID(r.URL.Query().Get("ps")))
	}
	if limit == 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM v3_identity.api_keys WHERE user_id=$1 AND deleted_at IS NULL`, uid).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, keySelect+` WHERE k.user_id=$1 AND k.deleted_at IS NULL ORDER BY k.id DESC LIMIT $2 OFFSET $3`, uid, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	items := []identity.KeyRecord{}
	for rows.Next() {
		k, err := scanOwnedKey(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return map[string]any{"page": page, "page_size": limit, "total": total, "items": items}, nil
}

func (s *Service) keyHTTP(w http.ResponseWriter, r *http.Request) {
	scope := "tokens:read"
	if r.Method == "PUT" {
		scope = "tokens:write"
	}
	d, ok := s.device(w, r, scope)
	if !ok {
		return
	}
	id := positiveID(r.PathValue("id"))
	if r.Method == "POST" {
		raw, err := s.id.RevealKey(r.Context(), d.UserID, id)
		reply(w, map[string]string{"key": raw}, err)
		return
	}
	var in struct {
		Group string `json:"group"`
	}
	if err := decode(w, r, &in); err != nil {
		reply(w, nil, err)
		return
	}
	k, err := s.loadKey(r.Context(), d.UserID, id)
	if err != nil {
		reply(w, nil, err)
		return
	}
	k.Group = &in.Group
	if err = s.id.UpdateKey(r.Context(), d.UserID, k.KeyInput); err != nil {
		reply(w, nil, err)
		return
	}
	k, err = s.loadKey(r.Context(), d.UserID, id)
	var dto any
	if err == nil {
		dto, err = keyDTO(r, k)
	}
	reply(w, dto, err)
}

func (s *Service) ensureHTTP(w http.ResponseWriter, r *http.Request) {
	d, ok := s.device(w, r, "tokens:write")
	if !ok {
		return
	}
	var in struct {
		DeviceName string `json:"device_name"`
		Group      string `json:"group"`
	}
	if err := decode(w, r, &in); err != nil {
		reply(w, nil, err)
		return
	}
	name := "Code Go Desktop - " + strings.TrimSpace(in.DeviceName)
	if strings.TrimSpace(in.DeviceName) == "" {
		name += "Default"
	}
	if len(name) > 100 {
		reply(w, nil, ErrInvalid)
		return
	}
	k, raw, created, err := s.ensureKey(r.Context(), d.UserID, name, in.Group)
	var dto any
	if err == nil {
		dto, err = keyDTO(r, k)
	}
	reply(w, map[string]any{"token": dto, "created": created, "full_key": raw, "token_name": name}, err)
}
