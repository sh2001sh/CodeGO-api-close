package channelmarket

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Verification struct {
	ID        string      `json:"id"`
	ChannelID int64       `json:"channel_id"`
	Status    string      `json:"status"`
	Results   []ModelTest `json:"results"`
}

func (s *Service) QueueVerification(ctx context.Context, a Actor, channel int64) (Verification, error) {
	result := Verification{ChannelID: channel, Status: "queued", Results: []ModelTest{}}
	id, err := newID()
	if err != nil {
		return result, err
	}
	result.ID = id
	err = s.transaction(ctx, func(tx pgx.Tx) error {
		group, e := owned(ctx, tx, a, channel)
		if e != nil {
			return e
		}
		e = tx.QueryRow(ctx, `INSERT INTO v3_channelmarket.verification_runs(id,channel_id) VALUES($1,$2) ON CONFLICT(channel_id) WHERE status IN ('queued','running') DO UPDATE SET channel_id=EXCLUDED.channel_id RETURNING id,status`, id, channel).Scan(&result.ID, &result.Status)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE v3_channelmarket.groups SET lifecycle_status=CASE WHEN lifecycle_status='active' THEN lifecycle_status ELSE 'verifying' END,verification_status=CASE WHEN lifecycle_status='active' THEN verification_status ELSE $2 END WHERE id=$1`, group, result.Status); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE v3_catalog.channels SET status='disabled' WHERE id=$1 AND NOT EXISTS(SELECT 1 FROM v3_channelmarket.groups WHERE channel_id=$1 AND lifecycle_status='active')`, channel); e != nil {
			return e
		}
		return syncCommunity(ctx, tx, channel)
	})
	return result, err
}

func (s *Service) PauseVerification(ctx context.Context, a Actor, channel int64) error {
	return s.transaction(ctx, func(tx pgx.Tx) error {
		group, e := owned(ctx, tx, a, channel)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE v3_channelmarket.verification_runs SET status='paused',completed_at=$2 WHERE channel_id=$1 AND status IN ('queued','running')`, channel, s.cfg.Now()); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE v3_channelmarket.groups SET verification_status='paused' WHERE id=$1 AND lifecycle_status<>'active'`, group); e != nil {
			return e
		}
		return syncCommunity(ctx, tx, channel)
	})
}

// pendingVerificationRun is one claimed verification_runs row along with the
// probe request fields needed to run it.
type pendingVerificationRun struct {
	run       Verification
	probe     ProbeRequest
	encrypted []byte
	models    []string
	trigger   string
}

func (s *Service) ProcessVerification(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	decrypter, ok := s.enc.(interface{ Decrypt([]byte) ([]byte, error) })
	if !ok {
		return 0, ErrUnavailable
	}
	done := 0
	for done < limit {
		pending, err := s.claimNextVerificationRunTx(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return done, err
		}
		runCtx, cancel := context.WithCancel(ctx)
		watchDone := make(chan error, 1)
		go s.watchVerification(runCtx, cancel, pending.run.ID, watchDone)
		results, status, err := s.runVerificationProbesTx(runCtx, decrypter, pending)
		cancel()
		if watchErr := <-watchDone; watchErr != nil && ctx.Err() == nil {
			return done, watchErr
		}
		if err != nil {
			if ctx.Err() == nil && errors.Is(err, context.Canceled) {
				// An owner paused/deleted the channel or disabled the automatic
				// probe while its upstream request was in flight.
				done++
				continue
			}
			return done, err
		}
		if err = s.saveVerificationResultTx(ctx, pending, results, status); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// Settings changes are committed by the control process, so cancellation must
// be observed across processes. A single watcher per run also interrupts a slow
// upstream request rather than waiting for the next declared model.
func (s *Service) watchVerification(ctx context.Context, cancel context.CancelFunc, id string, done chan<- error) {
	var failure error
	defer func() { done <- failure }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var running bool
		err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_channelmarket.verification_runs v JOIN v3_catalog.channels c ON c.id=v.channel_id JOIN v3_channelmarket.groups g ON g.channel_id=c.id WHERE v.id=$1 AND v.status='running' AND g.deleted_at IS NULL AND (v.trigger<>'auto_probe' OR (c.status='enabled' AND g.lifecycle_status='active' AND c.settings->'market'->>'auto_probe_enabled'='true')))`, id).Scan(&running)
		if err != nil || !running {
			if err != nil && ctx.Err() == nil {
				failure = err
			}
			cancel()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// claimNextVerificationRunTx reclaims stale running runs, then locks and
// marks the oldest queued run as running.
func (s *Service) claimNextVerificationRunTx(ctx context.Context) (pendingVerificationRun, error) {
	var pending pendingVerificationRun
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE v3_channelmarket.verification_runs v SET status='paused',completed_at=$1,summary='Automatic probe disabled or channel inactive' WHERE v.trigger='auto_probe' AND v.status IN ('queued','running') AND NOT EXISTS(SELECT 1 FROM v3_catalog.channels c JOIN v3_channelmarket.groups g ON g.channel_id=c.id WHERE c.id=v.channel_id AND c.status='enabled' AND g.lifecycle_status='active' AND g.deleted_at IS NULL AND c.settings->'market'->>'auto_probe_enabled'='true')`, s.cfg.Now()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `UPDATE v3_channelmarket.verification_runs SET status='queued',started_at=NULL WHERE status='running' AND started_at<$1`, s.cfg.Now().Add(-time.Hour)); e != nil {
			return e
		}
		e := tx.QueryRow(ctx, `SELECT v.id,v.channel_id,c.provider,c.base_url,k.secret,CASE WHEN v.target_model<>'' THEN ARRAY[v.target_model] ELSE ARRAY(SELECT model FROM v3_catalog.channel_models WHERE channel_id=c.id ORDER BY model) END,v.trigger FROM v3_channelmarket.verification_runs v JOIN v3_catalog.channels c ON c.id=v.channel_id JOIN v3_channelmarket.groups g ON g.channel_id=c.id AND g.deleted_at IS NULL AND g.lifecycle_status<>'deleted' LEFT JOIN LATERAL(SELECT secret FROM v3_catalog.channel_credentials WHERE channel_id=c.id AND status='enabled' AND (expires_at IS NULL OR expires_at>$1) ORDER BY id LIMIT 1) k ON true WHERE v.status='queued' AND (v.trigger<>'auto_probe' OR (c.status='enabled' AND g.lifecycle_status='active' AND c.settings->'market'->>'auto_probe_enabled'='true')) ORDER BY v.created_at,v.id LIMIT 1 FOR UPDATE OF v SKIP LOCKED`, s.cfg.Now()).Scan(&pending.run.ID, &pending.run.ChannelID, &pending.probe.Provider, &pending.probe.BaseURL, &pending.encrypted, &pending.models, &pending.trigger)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `UPDATE v3_channelmarket.verification_runs SET status='running',stage='connectivity',started_at=$2 WHERE id=$1 AND status='queued'`, pending.run.ID, s.cfg.Now())
		return e
	})
	return pending, err
}

// runVerificationProbesTx decrypts the channel credential and probes every
// declared model, cross-checking against the upstream model listing when
// available. It returns the per-model results and the overall pass/fail status.
func (s *Service) runVerificationProbesTx(ctx context.Context, decrypter interface{ Decrypt([]byte) ([]byte, error) }, pending pendingVerificationRun) ([]ModelTest, string, error) {
	p := pending.probe
	secret, err := decrypter.Decrypt(pending.encrypted)
	if err != nil || len(secret) == 0 {
		results := make([]ModelTest, 0, len(pending.models))
		for _, model := range pending.models {
			results = append(results, ModelTest{Model: model, Status: "failed", Error: "渠道凭据不可用", TestedAt: s.cfg.Now()})
		}
		return results, "failed", nil
	}
	p.Secret = string(secret)
	passed := len(pending.models) > 0
	var listed map[string]bool
	if s.cfg.ListModels != nil {
		available, listErr := s.cfg.ListModels(ctx, FetchModelsRequest{Provider: p.Provider, BaseURL: p.BaseURL, APIKey: p.Secret})
		listed = map[string]bool{}
		for _, model := range available {
			listed[strings.ToLower(model)] = true
		}
		if listErr != nil {
			passed = false
		}
	}
	results := make([]ModelTest, 0, len(pending.models))
	for _, model := range pending.models {
		p.Model = model
		result, e := s.cfg.Probe(ctx, p)
		result.Model = model
		if result.TestedAt.IsZero() {
			result.TestedAt = s.cfg.Now()
		}
		if listed != nil {
			result.Listed = listed[strings.ToLower(model)]
			if !result.Listed {
				passed = false
				result.Status = "failed"
				result.Error = "上游模型列表未包含声明模型"
			}
		}
		if e != nil {
			passed = false
			result.Status = "failed"
			result.Error = "上游连通性或响应验证失败"
		}
		if result.Status != "passed" {
			passed = false
		}
		results = append(results, result)
		if ctx.Err() != nil {
			return results, "", ctx.Err()
		}
	}
	status := "failed"
	if passed {
		status = "passed"
	}
	return results, status, nil
}

// saveVerificationResultTx persists the probe results onto the run and, for
// non-auto-probe runs, propagates the status onto the market group.
func (s *Service) saveVerificationResultTx(ctx context.Context, pending pendingVerificationRun, results []ModelTest, status string) error {
	payload, err := json.Marshal(results)
	if err != nil {
		return err
	}
	return s.transaction(ctx, func(tx pgx.Tx) error {
		// Deletion may happen while an upstream probe is in flight. Lock both
		// records before accepting its result so deleted history stays terminal.
		var group string
		e := tx.QueryRow(ctx, `SELECT g.id FROM v3_channelmarket.groups g JOIN v3_catalog.channels c ON c.id=g.channel_id WHERE g.channel_id=$1 AND g.deleted_at IS NULL AND g.lifecycle_status<>'deleted' FOR UPDATE OF g,c`, pending.run.ChannelID).Scan(&group)
		if errors.Is(e, pgx.ErrNoRows) {
			_, e = tx.Exec(ctx, `UPDATE v3_channelmarket.verification_runs SET status='paused',completed_at=$2,summary='Channel deleted before verification completed' WHERE id=$1 AND status='running'`, pending.run.ID, s.cfg.Now())
			return e
		}
		if e != nil {
			return e
		}
		tag, e := tx.Exec(ctx, `UPDATE v3_channelmarket.verification_runs SET status=$2,stage='completed',results=$3,completed_at=$4,summary='Connectivity probe; publication requires administrator approval' WHERE id=$1 AND status='running'`, pending.run.ID, status, payload, s.cfg.Now())
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if pending.trigger == "auto_probe" {
			_, e = tx.Exec(ctx, `UPDATE v3_catalog.channels SET settings=jsonb_set(settings,'{market}',coalesce(settings->'market','{}')||jsonb_build_object('auto_probe_last_status',$2::text,'auto_probe_last_at',$3::timestamptz)) WHERE id=$1`, pending.run.ChannelID, status, s.cfg.Now())
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE v3_channelmarket.groups SET verification_status=$2 WHERE channel_id=$1 AND deleted_at IS NULL AND lifecycle_status<>'active'`, pending.run.ChannelID, status); e != nil {
			return e
		}
		return syncCommunity(ctx, tx, pending.run.ChannelID)
	})
}

func (s *Service) httpVerify(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	result, err := s.QueueVerification(r.Context(), a, channel)
	s.result(w, result, err)
}
func (s *Service) httpPauseVerify(w http.ResponseWriter, r *http.Request, a Actor) {
	channel, ok := s.httpChannel(w, r, a)
	if !ok {
		return
	}
	s.result(w, nil, s.PauseVerification(r.Context(), a, channel))
}
