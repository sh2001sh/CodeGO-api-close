package channelmarket

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type WelfareRequest struct {
	UserIDs         []string `json:"user_ids"`
	Type            string   `json:"type"`
	Amount          int64    `json:"amount"`
	AmountMicro     int64    `json:"amount_micro"`
	OperationID     string   `json:"operation_id"`
	PaymentPassword string   `json:"payment_password,omitempty"`
}
type WelfareItem struct {
	UserID string `json:"user_id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
type WelfareResult struct {
	Success int           `json:"success_count"`
	Failed  int           `json:"failed_count"`
	Details []WelfareItem `json:"details"`
}

func (s *Service) Welfare(ctx context.Context, a Actor, channel int64, input WelfareRequest) (WelfareResult, error) {
	out := WelfareResult{Details: []WelfareItem{}}
	input, err := normalizeWelfareRequest(s, input)
	if err != nil {
		return out, err
	}
	if err := s.transaction(ctx, func(tx pgx.Tx) error { _, e := owned(ctx, tx, a, channel); return e }); err != nil {
		return out, err
	}
	for _, id := range input.UserIDs {
		out.Details = append(out.Details, s.sendWelfareItem(ctx, a, channel, input, id))
	}
	for _, item := range out.Details {
		if item.Status == "success" {
			out.Success++
		} else {
			out.Failed++
		}
	}
	return out, nil
}

// normalizeWelfareRequest validates and canonicalizes the welfare request:
// it derives AmountMicro's legacy fallback, validates the recipient list and
// per-type fields, and trims/dedupes user ids.
func normalizeWelfareRequest(s *Service, input WelfareRequest) (WelfareRequest, error) {
	if input.Type == "transfer" && input.AmountMicro == 0 && input.Amount > 0 && input.Amount <= math.MaxInt64/2 {
		input.AmountMicro = input.Amount * 2
	}
	if len(input.UserIDs) < 1 || len(input.UserIDs) > 100 || input.OperationID == "" || len(input.OperationID) > 64 || strings.ContainsRune(input.OperationID, 0) {
		return input, ErrInvalid
	}
	switch input.Type {
	case "blind_box":
		if input.Amount < 1 || input.Amount > 100 {
			return input, ErrInvalid
		}
		if s.cfg.GiftBoxes == nil {
			return input, ErrUnavailable
		}
	case "transfer":
		if input.AmountMicro <= 0 || input.PaymentPassword == "" {
			return input, ErrInvalid
		}
		if s.cfg.WelfareTransfer == nil {
			return input, ErrUnavailable
		}
	default:
		return input, ErrInvalid
	}
	seen := map[string]bool{}
	for i, raw := range input.UserIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			return input, ErrInvalid
		}
		seen[id] = true
		input.UserIDs[i] = id
	}
	return input, nil
}

// sendWelfareItem resolves one recipient and sends them the welfare grant,
// logging unexpected failures.
func (s *Service) sendWelfareItem(ctx context.Context, a Actor, channel int64, input WelfareRequest, id string) WelfareItem {
	item := WelfareItem{UserID: id, Status: "success"}
	var recipient int64
	var external string
	numeric, _ := strconv.ParseInt(id, 10, 64)
	err := s.pool.QueryRow(ctx, `SELECT id,coalesce(external_id,'') FROM v3_identity.users WHERE ((external_id=$1 AND $2=0) OR id=$2) AND status='active' AND deleted_at IS NULL`, id, numeric).Scan(&recipient, &external)
	if err == nil && recipient == a.UserID {
		err = ErrInvalid
	}
	if err == nil {
		op := "market-welfare:" + strconv.FormatInt(a.UserID, 10) + ":" + input.OperationID + ":" + strconv.FormatInt(recipient, 10)
		if input.Type == "blind_box" {
			_, err = s.cfg.GiftBoxes(ctx, a.UserID, recipient, op, int(input.Amount))
		} else if external == "" {
			err = ErrNotFound
		} else {
			err = s.cfg.WelfareTransfer(ctx, a.UserID, external, input.AmountMicro, input.PaymentPassword, op)
		}
	}
	if err != nil {
		item.Status = "failed"
		item.Error = "赠送失败：请核对接收用户、余额或库存及支付凭据"
		if !errors.Is(err, pgx.ErrNoRows) {
			s.log.Warn("market welfare item failed", "channel_id", channel, "actor_user_id", a.UserID, "recipient_user_id", recipient)
		}
	}
	return item
}
func (s *Service) httpWelfare(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	var input WelfareRequest
	if !decode(w, r, &input) {
		return
	}
	out, err := s.Welfare(r.Context(), a, channel, input)
	s.result(w, out, err)
}
