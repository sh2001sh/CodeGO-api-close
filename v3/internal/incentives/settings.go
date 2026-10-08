package incentives

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Numeric fields preserve decimal source precision and never pass through floats.
type Settings struct {
	Enabled          bool        `json:"enabled"`
	QuotaUnit        string      `json:"quota_unit"`
	Timezone         string      `json:"timezone"`
	DrawHour         int         `json:"draw_hour"`
	DrawMinute       int         `json:"draw_minute"`
	Base1            json.Number `json:"base_reward_1_usd"`
	Base2            json.Number `json:"base_reward_2_usd"`
	Base3            json.Number `json:"base_reward_3_usd"`
	Base4            json.Number `json:"base_reward_4_usd"`
	Lite             json.Number `json:"multiplier_lite"`
	Standard         json.Number `json:"multiplier_standard"`
	Pro              json.Number `json:"multiplier_pro"`
	Ultra            json.Number `json:"multiplier_ultra"`
	JackpotInitial   json.Number `json:"jackpot_initial_usd"`
	JackpotIncrement json.Number `json:"jackpot_increment_usd"`
	JackpotCap       json.Number `json:"jackpot_cap_usd"`
	Cost             json.Number `json:"cost_per_usd"`
	Budget           json.Number `json:"monthly_budget_usd"`
}

func defaultSettings() Settings {
	return Settings{true, "unified_credit", "Asia/Shanghai", 20, 0, "0.25", "2.5", "12.5", "25", "1", "1.1", "1.2", "1.3", "25", "5", "250", "0.1", "0"}
}
func (c Settings) validate() error {
	if c.QuotaUnit != "unified_credit" || c.DrawHour < 0 || c.DrawHour > 23 || c.DrawMinute < 0 || c.DrawMinute > 59 {
		return ErrInvalid
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return ErrInvalid
	}
	for _, v := range []json.Number{c.Base1, c.Base2, c.Base3, c.Base4, c.Lite, c.Standard, c.Pro, c.Ultra, c.JackpotInitial} {
		r, err := decimal(string(v))
		if err != nil || r.Sign() <= 0 || r.Cmp(big.NewRat(9_000_000_000_000, 1)) > 0 {
			return ErrInvalid
		}
	}
	for _, v := range []json.Number{c.JackpotIncrement, c.JackpotCap, c.Cost, c.Budget} {
		r, err := decimal(string(v))
		if err != nil || r.Sign() < 0 || r.Cmp(big.NewRat(9_000_000_000_000, 1)) > 0 {
			return ErrInvalid
		}
	}
	a, _ := new(big.Rat).SetString(string(c.JackpotInitial))
	b, _ := new(big.Rat).SetString(string(c.JackpotCap))
	if a.Cmp(b) > 0 {
		return ErrInvalid
	}
	maxBase := big.NewRat(0, 1)
	maxMultiplier := big.NewRat(1, 1)
	for _, n := range []json.Number{c.Base1, c.Base2, c.Base3, c.Base4} {
		v, _ := decimal(string(n))
		if v.Cmp(maxBase) > 0 {
			maxBase = v
		}
	}
	for _, n := range []json.Number{c.Lite, c.Standard, c.Pro, c.Ultra} {
		v, _ := decimal(string(n))
		if v.Cmp(maxMultiplier) > 0 {
			maxMultiplier = v
		}
	}
	if _, err := rewardRatMicro(maxBase, maxMultiplier, b); err != nil {
		return ErrInvalid
	}
	return nil
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func readSettings(ctx context.Context, q querier) (Settings, error) {
	c := defaultSettings()
	payload, _ := json.Marshal(c)
	fields := map[string]json.RawMessage{}
	_ = json.Unmarshal(payload, &fields)
	rows, err := q.Query(ctx, `SELECT key,value FROM v3_platform.settings WHERE key LIKE 'daily_lucky_number_setting.%' AND NOT sensitive`)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var raw []byte
		if err = rows.Scan(&key, &raw); err != nil {
			return c, err
		}
		key = strings.TrimPrefix(key, "daily_lucky_number_setting.")
		if _, ok := fields[key]; !ok {
			continue
		}
		var str string
		if json.Unmarshal(raw, &str) == nil && key != "timezone" && key != "quota_unit" {
			raw = []byte(str)
		}
		fields[key] = raw
	}
	if err = rows.Err(); err != nil {
		return c, err
	}
	payload, err = json.Marshal(fields)
	if err == nil {
		err = json.Unmarshal(payload, &c)
	}
	if err != nil {
		return c, ErrInvalid
	}
	return c, c.validate()
}
func (s *Service) Settings(ctx context.Context) (Settings, error) { return readSettings(ctx, s.pool) }
func (s *Service) UpdateSettings(ctx context.Context, patch map[string]json.RawMessage) (Settings, error) {
	var result Settings
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('incentives:settings',0))`); err != nil {
			return err
		}
		c, err := readSettings(ctx, tx)
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(c)
		m := map[string]json.RawMessage{}
		_ = json.Unmarshal(raw, &m)
		for k, v := range patch {
			if _, ok := m[k]; !ok || k == "quota_unit" {
				return ErrInvalid
			}
			m[k] = v
		}
		raw, err = json.Marshal(m)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &result); err != nil {
			return ErrInvalid
		}
		if err = result.validate(); err != nil {
			return err
		}
		for k, v := range patch {
			if _, err = tx.Exec(ctx, `INSERT INTO v3_platform.settings(key,value) VALUES($1,$2::jsonb) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, "daily_lucky_number_setting."+k, string(v)); err != nil {
				return err
			}
		}
		return nil
	})
	return result, err
}
