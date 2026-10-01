package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/billing"
)

type minuteEvidence struct {
	Minute   int64 `json:"minute"`
	Requests int64 `json:"requests"`
	Short    int64 `json:"short"`
	Input    int64 `json:"input"`
	Cache    int64 `json:"cache"`
}
type windowEvidence struct {
	WindowEnd int64            `json:"window_end"`
	Minutes   []minuteEvidence `json:"minutes"`
}

// RecordUsageTx matches ledger.UsageHook. Ledger invokes it only for fresh primary
// events. This queue insert takes no identity/domain/account locks, so it cannot
// invert the financial transaction's existing lock order.
func (g *Guard) RecordUsageTx(ctx context.Context, tx pgx.Tx, fields map[string]string) error {
	if !g.cfg.Enabled || fields[billing.FieldTerminal] != "completed" || fields[billing.FieldEstimated] != "0" || fields["funding_part"] == "secondary" {
		return nil
	}
	values := make([]int64, 4)
	for i, key := range []string{billing.FieldUserID, billing.FieldChannelID, billing.FieldPromptTokens, billing.FieldCachedTokens} {
		value, err := strconv.ParseInt(fields[key], 10, 64)
		if err != nil {
			return nil
		} // Missing/invalid telemetry is never proof of abuse.
		values[i] = value
	}
	user, channel, input, cache := values[0], values[1], values[2], values[3]
	model := fields[billing.FieldModel]
	if user <= 0 || input <= 0 || cache < 0 || cache > input || model == "" {
		return nil
	}
	request := fields[billing.FieldRequestID]
	if request == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO v3_security.usage_queue VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(request_id) DO NOTHING`, request, user, channel, model, input, cache, g.cfg.Now().Unix())
	return err
}

func (g *Guard) recordSampleTx(ctx context.Context, tx pgx.Tx, user, channel int64, model string, input, cache, observed int64) error {
	// Serialize an account's windows across every key/model. Administrators are exempt.
	var role string
	if err := tx.QueryRow(ctx, `SELECT role FROM v3_identity.users WHERE id=$1 FOR UPDATE`, user).Scan(&role); err != nil {
		return err
	}
	if role == "admin" || role == "root" {
		return nil
	}
	now := g.cfg.Now().Unix()
	minute := observed / 60
	hash := sha256.Sum256([]byte(model))
	modelHash := hex.EncodeToString(hash[:])
	if cache > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO v3_security.cache_support VALUES($1,$2,$3) ON CONFLICT(channel_id,model_hash) DO UPDATE SET expires_at=greatest(v3_security.cache_support.expires_at,excluded.expires_at)`, channel, modelHash, observed+86400); err != nil {
			return err
		}
	}
	var known bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_security.cache_support WHERE channel_id=$1 AND model_hash=$2 AND expires_at>$3)`, channel, modelHash, observed).Scan(&known); err != nil {
		return err
	}
	short, unknown := 0, 1
	if input <= 2048 {
		short = 1
	}
	if known {
		unknown = 0
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_security.usage_samples VALUES($1,$2,$3,1,$4,$5,$6,$7) ON CONFLICT(user_id,model_hash,minute) DO UPDATE SET requests=v3_security.usage_samples.requests+1,short_requests=v3_security.usage_samples.short_requests+excluded.short_requests,input_tokens=v3_security.usage_samples.input_tokens+excluded.input_tokens,cached_tokens=v3_security.usage_samples.cached_tokens+excluded.cached_tokens,unknown_requests=v3_security.usage_samples.unknown_requests+excluded.unknown_requests`, user, modelHash, minute, short, input, cache, unknown); err != nil {
		return err
	}
	evidence := windowEvidence{WindowEnd: minute * 60}
	for i := int64(2); i >= 1; i-- {
		var e minuteEvidence
		var unknown int64
		e.Minute = minute - i
		err := tx.QueryRow(ctx, `SELECT requests,short_requests,input_tokens,cached_tokens,unknown_requests FROM v3_security.usage_samples WHERE user_id=$1 AND model_hash=$2 AND minute=$3`, user, modelHash, e.Minute).Scan(&e.Requests, &e.Short, &e.Input, &e.Cache, &unknown)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if e.Requests < 60 || e.Short*10 < e.Requests*9 || e.Input <= 0 || e.Cache*100 >= e.Input*5 || unknown > 0 {
			return nil
		}
		evidence.Minutes = append(evidence.Minutes, e)
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	return g.applyEpisodeTx(ctx, tx, user, evidence.WindowEnd, string(raw), now)
}

func (g *Guard) applyEpisodeTx(ctx context.Context, tx pgx.Tx, user, window int64, evidence string, now int64) error {
	if window <= 0 || window > now {
		return errors.New("security: invalid observation window")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO v3_security.account_request_abuse_states VALUES($1,0,0,0,false,'',0) ON CONFLICT(user_id) DO NOTHING`, user); err != nil {
		return err
	}
	state, err := readState(ctx, tx, user)
	if err != nil {
		return err
	}
	if state.Blocked || window <= state.LastWindowEnd || (state.Strikes > 0 && (now < state.RestrictedUntil || window-180 < state.RestrictedUntil)) {
		return nil
	}
	state.Strikes++
	state.LastWindowEnd = window
	state.Evidence = evidence
	state.UpdatedAt = now
	if state.Strikes == 1 {
		state.RestrictedUntil = now + 86400
	} else {
		state.Blocked = true
		if _, err = tx.Exec(ctx, `UPDATE v3_identity.users SET status='disabled' WHERE id=$1`, user); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE v3_security.account_request_abuse_states SET strikes=$2,restricted_until=$3,last_window_end=$4,blocked=$5,evidence=$6,updated_at=$7 WHERE user_id=$1`, user, state.Strikes, state.RestrictedUntil, state.LastWindowEnd, state.Blocked, state.Evidence, now); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO v3_security.state_cache_refresh VALUES($1) ON CONFLICT DO NOTHING`, user)
	return err
}

// FlushStateCache publishes committed decisions, then acknowledges the durable
// outbox. On Redis failure the row remains pending and the worker reports error.
func (g *Guard) FlushStateCache(ctx context.Context, limit int) (int, error) {
	if !g.cfg.Enabled {
		return 0, nil
	}
	limit = min(max(limit, 1), 1000)
	if err := g.processUsage(ctx, limit); err != nil {
		return 0, err
	}
	count := 0
	err := pgx.BeginFunc(ctx, g.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT user_id FROM v3_security.state_cache_refresh ORDER BY user_id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit)
		if err != nil {
			return err
		}
		var users []int64
		for rows.Next() {
			var u int64
			if err = rows.Scan(&u); err != nil {
				rows.Close()
				return err
			}
			users = append(users, u)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return err
		}
		for _, user := range users {
			state, e := readState(ctx, tx, user)
			if e != nil {
				return e
			}
			raw, e := json.Marshal(state)
			if e != nil {
				return e
			}
			if e = g.redis.Set(ctx, stateKey(user), raw, time.Minute).Err(); e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `DELETE FROM v3_security.state_cache_refresh WHERE user_id=$1`, user); e != nil {
				return e
			}
			count++
		}
		// Expired samples cannot enter the two-complete-minute rule again.
		now := g.cfg.Now().Unix()
		if _, err = tx.Exec(ctx, `DELETE FROM v3_security.usage_samples WHERE (user_id,model_hash,minute) IN (SELECT user_id,model_hash,minute FROM v3_security.usage_samples WHERE minute<$1 LIMIT 1000 FOR UPDATE SKIP LOCKED)`, now/60-5); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM v3_security.cache_support WHERE (channel_id,model_hash) IN (SELECT channel_id,model_hash FROM v3_security.cache_support WHERE expires_at<=$1 LIMIT 1000 FOR UPDATE SKIP LOCKED)`, now)
		return err
	})
	return count, err
}
