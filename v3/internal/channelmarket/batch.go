package channelmarket

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type BatchRequest struct {
	GroupIDs []string `json:"group_ids"`
	Model    string   `json:"model"`
}

// BatchRelayRequest must enter the authenticated gateway's normal billing path.
// RequestID is stable for reconciliation; an uncertain call must never be replayed.
type BatchRelayRequest struct {
	UserID    int64
	Group     string
	Model     string
	RequestID string
}
type BatchReceipt struct {
	RequestID     string `json:"request_id"`
	AmountMicro   int64  `json:"amount_micro"`
	BillingSource string `json:"billing_source"`
	LogCreated    bool   `json:"log_created"`
}
type BatchItem struct {
	GroupID   string `json:"group_id"`
	GroupName string `json:"group_name"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	BatchReceipt
	Error     string     `json:"error,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}
type BatchView struct {
	ID           string      `json:"id"`
	Model        string      `json:"model"`
	Status       string      `json:"status"`
	BillingMode  string      `json:"billing_mode"`
	QuotaCharged bool        `json:"quota_charged"`
	LogCreated   bool        `json:"log_created"`
	Items        []BatchItem `json:"items"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

func (s *Service) StartBatch(ctx context.Context, user int64, r BatchRequest) (BatchView, error) {
	var v BatchView
	r.Model = strings.TrimSpace(r.Model)
	if user <= 0 || r.Model == "" || len(r.Model) > 128 || len(r.GroupIDs) < 1 || len(r.GroupIDs) > 5 {
		return v, ErrInvalid
	}
	if s.cfg.BatchRelay == nil {
		return v, ErrUnavailable
	}
	id, err := newID()
	if err != nil {
		return v, err
	}
	err = s.transaction(ctx, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO v3_channelmarket.batch_tests(id,owner_user_id,model) VALUES($1,$2,$3)`, id, user, r.Model); e != nil {
			return e
		}
		seen := map[string]bool{}
		for _, raw := range r.GroupIDs {
			group := strings.TrimSpace(raw)
			if group == "" || seen[group] {
				return ErrInvalid
			}
			seen[group] = true
			internal, name, e := batchTarget(ctx, tx, user, group, r.Model)
			if e != nil {
				return e
			}
			request, e := newID()
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO v3_channelmarket.batch_test_items(batch_id,group_id,group_name,internal_group_name,request_id) VALUES($1,$2,$3,$4,$5)`, id, group, name, internal, "market-test-"+request); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return v, err
	}
	return s.Batch(ctx, user, id)
}
func batchTarget(ctx context.Context, tx pgx.Tx, user int64, group, model string) (string, string, error) {
	var internal, name string
	if strings.HasPrefix(group, "official:") {
		internal = strings.TrimPrefix(group, "official:")
		name = internal
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_identity.allowed_groups($1) a JOIN v3_catalog.channel_groups cg ON cg.group_name=a JOIN v3_catalog.channels c ON c.id=cg.channel_id JOIN v3_catalog.channel_models m ON m.channel_id=c.id WHERE a=$2 AND c.scope='official' AND c.status='enabled' AND m.model=$3)`, user, internal, model).Scan(&allowed)
		if err != nil {
			return "", "", err
		}
		if !allowed {
			return "", "", ErrNotFound
		}
		return internal, name, nil
	}
	if err := accessible(ctx, tx, user, group); err != nil {
		return "", "", err
	}
	err := tx.QueryRow(ctx, `SELECT g.internal_group_name,g.display_name FROM v3_channelmarket.groups g JOIN v3_catalog.channel_models m ON m.channel_id=g.channel_id WHERE g.id=$1 AND m.model=$2`, group, model).Scan(&internal, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return internal, name, err
}
func (s *Service) Batch(ctx context.Context, user int64, id string) (BatchView, error) {
	v := BatchView{BillingMode: "user_quota", Items: []BatchItem{}}
	if s.pool == nil {
		return v, ErrUnavailable
	}
	err := s.pool.QueryRow(ctx, `SELECT id,model,status,created_at,updated_at FROM v3_channelmarket.batch_tests WHERE id=$1 AND owner_user_id=$2`, id, user).Scan(&v.ID, &v.Model, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	rows, err := s.pool.Query(ctx, `SELECT group_id,group_name,status,latency_ms,amount_micro,request_id,billing_source,log_created,error_message,started_at,ended_at FROM v3_channelmarket.batch_test_items WHERE batch_id=$1 ORDER BY group_id`, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var i BatchItem
		if err = rows.Scan(&i.GroupID, &i.GroupName, &i.Status, &i.LatencyMS, &i.AmountMicro, &i.RequestID, &i.BillingSource, &i.LogCreated, &i.Error, &i.StartedAt, &i.EndedAt); err != nil {
			return v, err
		}
		v.Items = append(v.Items, i)
		v.QuotaCharged = v.QuotaCharged || i.AmountMicro > 0
		v.LogCreated = v.LogCreated || i.LogCreated
	}
	return v, rows.Err()
}
func (s *Service) httpStartBatch(w http.ResponseWriter, r *http.Request, a Actor) {
	var input BatchRequest
	if !decode(w, r, &input) {
		return
	}
	v, err := s.StartBatch(r.Context(), a.UserID, input)
	s.result(w, v, err)
}
func (s *Service) httpBatch(w http.ResponseWriter, r *http.Request, a Actor) {
	v, err := s.Batch(r.Context(), a.UserID, r.PathValue("id"))
	s.result(w, v, err)
}
