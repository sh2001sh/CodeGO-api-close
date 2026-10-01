package marketplace

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type boxAdminInput struct {
	RequestID      *string `json:"request_id"`
	IdempotencyKey *string `json:"idempotency_key"`
	PoolID         *int64  `json:"pool_id"`
	Count          *int    `json:"count"`
	Quantity       *int    `json:"quantity"`
	Reason         string  `json:"reason"`
}

type boxGiftInput struct {
	RequestID string  `json:"request_id"`
	Recipient *int64  `json:"recipient_id"`
	External  *string `json:"recipient_external_id"`
	Count     int     `json:"count"`
}

func (s *Service) registerBlindBoxCompat(register func(string, bool, func(http.ResponseWriter, *http.Request, int64))) {
	register("POST /api/blind-box/inventory/purchase", false, s.handleCompatPurchase)
	register("POST /api/blind-box/inventory/gift", false, s.handleCompatGift)
	register("POST /api/blind-box/props/{id}/gift", false, s.handleCompatPropGift)
	register("POST /api/blind-box/admin/users/{id}/grants", true, s.handleCompatGrant)
	revoke := s.handleCompatRevoke
	register("POST /api/blind-box/admin/users/{id}/revoke", true, revoke)
	register("DELETE /api/blind-box/admin/users/{id}/grants", true, revoke)
	register("POST /api/blind-box/simulation/draw", false, s.handleCompatSimulateDraw)
}

func (s *Service) handleCompatPurchase(w http.ResponseWriter, r *http.Request, id int64) {
	var in struct {
		RequestID string `json:"request_id"`
		PoolID    *int64 `json:"pool_id"`
		Count     int    `json:"count"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := validRequest(id, in.RequestID, in.Count); err != nil {
		reply(w, nil, err)
		return
	}
	pool, err := s.compatPool(r.Context(), in.PoolID)
	if err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.PurchaseBoxes(r.Context(), id, in.RequestID, pool, in.Count)
	reply(w, out, err)
}

func (s *Service) handleCompatGift(w http.ResponseWriter, r *http.Request, id int64) {
	var in boxGiftInput
	if !decode(w, r, &in) {
		return
	}
	if err := validRequest(id, in.RequestID, in.Count); err != nil {
		reply(w, nil, err)
		return
	}
	recipient, err := s.compatRecipient(r.Context(), in.Recipient, in.External)
	if err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.GiftBoxes(r.Context(), id, recipient, in.RequestID, in.Count)
	reply(w, out, err)
}

func (s *Service) handleCompatPropGift(w http.ResponseWriter, r *http.Request, id int64) {
	var in boxGiftInput
	if !decode(w, r, &in) {
		return
	}
	if err := validRequest(id, in.RequestID, 1); err != nil {
		reply(w, nil, err)
		return
	}
	recipient, err := s.compatRecipient(r.Context(), in.Recipient, in.External)
	if err != nil {
		reply(w, nil, err)
		return
	}
	err = s.GiftProp(r.Context(), id, recipient, pathID(r), in.RequestID)
	reply(w, true, err)
}

func (s *Service) handleCompatGrant(w http.ResponseWriter, r *http.Request, adminID int64) {
	var in boxAdminInput
	if !decode(w, r, &in) {
		return
	}
	count, request, err := in.compatValues(r, false)
	if err != nil {
		reply(w, nil, err)
		return
	}
	pool, err := s.compatPool(r.Context(), in.PoolID)
	if err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.GrantBoxesWithReason(r.Context(), adminID, pathID(r), request, pool, count, in.Reason)
	reply(w, out, err)
}

func (s *Service) handleCompatRevoke(w http.ResponseWriter, r *http.Request, adminID int64) {
	var in boxAdminInput
	if !decode(w, r, &in) {
		return
	}
	count, request, err := in.compatValues(r, true)
	if err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.RevokeBoxesWithReason(r.Context(), adminID, pathID(r), request, count, in.Reason)
	w.Header().Set("Idempotency-Key", request)
	if err != nil {
		reply(w, nil, err)
		return
	}
	if r.Method == http.MethodDelete || in.Quantity != nil && in.Count == nil {
		reply(w, struct {
			Revoked int     `json:"revoked"`
			ItemIDs []int64 `json:"item_ids"`
		}{len(out), out}, err)
		return
	}
	reply(w, out, err)
}

func (s *Service) handleCompatSimulateDraw(w http.ResponseWriter, r *http.Request, _ int64) {
	var in struct {
		PoolID *int64    `json:"pool_id"`
		Count  int       `json:"count"`
		State  PityState `json:"state"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Count < 1 || in.Count > 100 || in.State.Opened < 0 || in.State.SmallProgress < 0 || in.State.BigProgress < 0 {
		reply(w, nil, ErrInvalidInput)
		return
	}
	pool, err := s.compatPool(r.Context(), in.PoolID)
	if err != nil {
		reply(w, nil, err)
		return
	}
	draws, state, err := s.Simulate(r.Context(), pool, in.Count, in.State)
	reply(w, struct {
		Draws []OpenRecord `json:"draws"`
		State PityState    `json:"state"`
	}{draws, state}, err)
}

func (s *Service) compatPool(ctx context.Context, provided *int64) (int64, error) {
	if provided != nil {
		if *provided <= 0 {
			return 0, ErrInvalidInput
		}
		return *provided, nil
	}
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM v3_marketplace.blind_box_pools WHERE scope='credits' AND enabled ORDER BY id LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUnavailable
	}
	return id, err
}

func (s *Service) compatRecipient(ctx context.Context, numeric *int64, external *string) (int64, error) {
	if numeric == nil && external == nil || numeric != nil && *numeric <= 0 {
		return 0, ErrInvalidInput
	}
	var id int64
	if external != nil {
		value := strings.ToUpper(strings.TrimSpace(*external))
		if value == "" {
			return 0, ErrInvalidInput
		}
		err := s.pool.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE external_id=$1 AND status='active' AND deleted_at IS NULL`, value).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		if err != nil {
			return 0, err
		}
		if numeric != nil && *numeric != id {
			return 0, ErrInvalidInput
		}
		return id, nil
	}
	err := s.pool.QueryRow(ctx, `SELECT id FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL`, *numeric).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

func (in boxAdminInput) compatValues(r *http.Request, random bool) (int, string, error) {
	if in.Count != nil && in.Quantity != nil && *in.Count != *in.Quantity || in.RequestID != nil && in.IdempotencyKey != nil && *in.RequestID != *in.IdempotencyKey || len(in.Reason) > 2000 {
		return 0, "", ErrInvalidInput
	}
	var count int
	if in.Count != nil {
		count = *in.Count
	} else if in.Quantity != nil {
		count = *in.Quantity
	}
	request := r.Header.Get("Idempotency-Key")
	provided := in.RequestID
	if provided == nil {
		provided = in.IdempotencyKey
	}
	if provided != nil {
		if request != "" && request != *provided {
			return 0, "", ErrInvalidInput
		}
		request = *provided
	}
	if strings.TrimSpace(request) == "" {
		if !random || provided != nil {
			return 0, "", ErrInvalidInput
		}
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			return 0, "", err
		}
		request = hex.EncodeToString(token[:])
	}
	if len(request) > 64 && in.RequestID == nil {
		if len(request) > 128 {
			return 0, "", ErrInvalidInput
		}
		digest := sha256.Sum256([]byte(request))
		request = hex.EncodeToString(digest[:])
	}
	if count < 1 || count > 100 || len(request) > 64 {
		return 0, "", ErrInvalidInput
	}
	return count, request, nil
}
