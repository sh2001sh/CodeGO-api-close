package channelmarket

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// dueAutoProbe is a channel whose auto-probe interval has elapsed, along
// with the model to probe.
type dueAutoProbe struct {
	channel int64
	model   string
}

// QueueAutoProbes schedules the owner-configured recurring connectivity check.
// It records observations without withdrawing an approved channel from traffic.
func (s *Service) QueueAutoProbes(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		limit = 10
	}
	count := 0
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		items, e := loadDueAutoProbesTx(ctx, tx, limit, s.cfg.Now())
		if e != nil {
			return e
		}
		for _, item := range items {
			id, e := newID()
			if e != nil {
				return e
			}
			tag, e := tx.Exec(ctx, `INSERT INTO v3_channelmarket.verification_runs(id,channel_id,trigger,target_model,created_at) VALUES($1,$2,'auto_probe',$3,$4) ON CONFLICT(channel_id) WHERE status IN ('queued','running') DO NOTHING`, id, item.channel, item.model, s.cfg.Now())
			if e != nil {
				return e
			}
			count += int(tag.RowsAffected())
		}
		return nil
	})
	return count, err
}

// loadDueAutoProbesTx locks up to limit enabled channels whose auto-probe
// interval has elapsed and resolves the model each should be probed with.
func loadDueAutoProbesTx(ctx context.Context, tx pgx.Tx, limit int, now time.Time) ([]dueAutoProbe, error) {
	rows, err := tx.Query(ctx, `SELECT c.id,coalesce(c.settings->'market','{}'),(SELECT max(created_at) FROM v3_channelmarket.verification_runs WHERE channel_id=c.id AND trigger='auto_probe'),ARRAY(SELECT model FROM v3_catalog.channel_models WHERE channel_id=c.id ORDER BY model) FROM v3_catalog.channels c JOIN v3_channelmarket.groups g ON g.channel_id=c.id WHERE c.status='enabled' AND g.lifecycle_status='active' AND g.deleted_at IS NULL AND c.settings->'market'->>'auto_probe_enabled'='true' AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.verification_runs WHERE channel_id=c.id AND status IN ('queued','running'))
	AND coalesce((SELECT max(created_at) FROM v3_channelmarket.verification_runs WHERE channel_id=c.id AND trigger='auto_probe'),nullif(c.settings->'market'->>'auto_probe_last_at','')::timestamptz,'-infinity'::timestamptz)<=$2::timestamptz-make_interval(mins=>coalesce(nullif((c.settings->'market'->>'auto_probe_interval_minutes')::integer,0),30))
	ORDER BY c.id LIMIT $1 FOR UPDATE OF c SKIP LOCKED`, limit, now)
	if err != nil {
		return nil, err
	}
	items := []dueAutoProbe{}
	for rows.Next() {
		var channel int64
		var raw []byte
		var last *time.Time
		var models []string
		if err = rows.Scan(&channel, &raw, &last, &models); err != nil {
			rows.Close()
			return nil, err
		}
		var config struct {
			Interval int        `json:"auto_probe_interval_minutes"`
			Model    string     `json:"auto_probe_model"`
			Last     *time.Time `json:"auto_probe_last_at"`
		}
		if err = json.Unmarshal(raw, &config); err != nil {
			rows.Close()
			return nil, err
		}
		if config.Interval <= 0 {
			config.Interval = 30
		}
		if last == nil {
			last = config.Last
		}
		if last != nil && last.Add(time.Duration(config.Interval)*time.Minute).After(now) {
			continue
		}
		if config.Model == "" && len(models) > 0 {
			config.Model = models[0]
		}
		if config.Model == "" {
			continue
		}
		items = append(items, dueAutoProbe{channel, config.Model})
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
