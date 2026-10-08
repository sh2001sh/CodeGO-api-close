package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type taskDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Pool accepts a pgxpool.Pool in production or a pgx.Tx for isolated tests.
type PostgresRepository struct{ Pool taskDB }

const columns = `id,user_id,key_id,group_name,target_group,provider,channel_id,credential_id,model,
 upstream_model,upstream_id,action,status,request_body,pricing_headers,reservation,provider_data,result_url,
 error_message,usage,units,cost_state,actual_credits,created_at,updated_at,lease_id`

func (p *PostgresRepository) Create(ctx context.Context, t Task) error {
	reservation, err := json.Marshal(t.Reservation)
	if err != nil {
		return err
	}
	headers, err := json.Marshal(t.PricingHeaders)
	if err != nil {
		return err
	}
	_, err = p.Pool.Exec(ctx, `INSERT INTO v3_workflow.tasks
 (id,user_id,key_id,group_name,provider,channel_id,credential_id,model,upstream_model,action,
 status,request_body,reservation,created_at,updated_at,lease_id,lease_until,pricing_headers,target_group)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14,$15,$16,$17,$18)`,
		t.ID, t.UserID, t.KeyID, t.Group, t.Provider, t.ChannelID, t.CredentialID, t.Model, t.UpstreamModel,
		t.Action, t.Status, t.Body, reservation, t.CreatedAt, t.LeaseID, t.CreatedAt.Add(5*time.Minute), headers, t.TargetGroup)
	if err != nil {
		return fmt.Errorf("create workflow task: %w", err)
	}
	return nil
}

func (p *PostgresRepository) GetOwned(ctx context.Context, id string, userID int64) (Task, error) {
	t, err := scan(p.Pool.QueryRow(ctx, `SELECT `+columns+` FROM v3_workflow.tasks WHERE id=$1 AND user_id=$2`, id, userID))
	if !errors.Is(err, ErrNotFound) {
		return t, err
	}
	err = p.Pool.QueryRow(ctx, `SELECT id,user_id,group_name,provider,channel_id,model,upstream_model,upstream_id,
	 action,status,provider_data,result_url,error_message,actual_credits,created_at,updated_at
	 FROM v3_workflow.legacy_tasks WHERE id=$1 AND user_id=$2`, id, userID).Scan(
		&t.ID, &t.UserID, &t.Group, &t.Provider, &t.ChannelID, &t.Model, &t.UpstreamModel, &t.UpstreamID,
		&t.Action, &t.Status, &t.Data, &t.URL, &t.Error, &t.ActualCredits, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	t.Historical, t.CostState = true, "settled"
	if t.Status == "failed" {
		t.CostState = "refunded"
	}
	return t, nil
}

func (p *PostgresRepository) Pending(ctx context.Context, limit int) ([]Task, error) {
	rows, err := p.Pool.Query(ctx, `SELECT `+columns+` FROM v3_workflow.tasks
 WHERE cost_state='reserved' AND status IN ('queued','in_progress','completed','failed')
 AND (lease_until IS NULL OR lease_until < now()) ORDER BY updated_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Task, 0)
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *PostgresRepository) Claim(ctx context.Context, id, lease string, until time.Time) (Task, error) {
	t, err := scan(p.Pool.QueryRow(ctx, `UPDATE v3_workflow.tasks SET lease_id=$2,lease_until=$3
 WHERE id=$1 AND cost_state='reserved' AND status IN ('queued','in_progress','completed','failed')
 AND (lease_until IS NULL OR lease_until < now()) RETURNING `+columns, id, lease, until))
	if errors.Is(err, ErrNotFound) {
		return Task{}, ErrConflict
	}
	return t, err
}

func (p *PostgresRepository) Save(ctx context.Context, t Task) error {
	usage, err := json.Marshal(t.Usage)
	if err != nil {
		return err
	}
	data := t.Data
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	result, err := p.Pool.Exec(ctx, `UPDATE v3_workflow.tasks SET upstream_id=$2,status=$3,
 provider_data=$4,result_url=$5,error_message=$6,usage=$7,units=$8,cost_state=$9,
 actual_credits=$10,updated_at=$11,lease_id='',lease_until=NULL
 WHERE id=$1 AND lease_id=$12`, t.ID, t.UpstreamID, t.Status, data, t.URL, t.Error, usage, t.Units,
		t.CostState, t.ActualCredits, t.UpdatedAt, t.LeaseID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func scan(row pgx.Row) (Task, error) {
	var t Task
	var reservation, usage, headers []byte
	err := row.Scan(&t.ID, &t.UserID, &t.KeyID, &t.Group, &t.TargetGroup, &t.Provider, &t.ChannelID, &t.CredentialID, &t.Model,
		&t.UpstreamModel, &t.UpstreamID, &t.Action, &t.Status, &t.Body, &headers, &reservation, &t.Data, &t.URL,
		&t.Error, &usage, &t.Units, &t.CostState, &t.ActualCredits, &t.CreatedAt, &t.UpdatedAt, &t.LeaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	if err = json.Unmarshal(reservation, &t.Reservation); err != nil {
		return Task{}, err
	}
	if err = json.Unmarshal(usage, &t.Usage); err != nil {
		return Task{}, err
	}
	if err = json.Unmarshal(headers, &t.PricingHeaders); err != nil {
		return Task{}, err
	}
	return t, nil
}
