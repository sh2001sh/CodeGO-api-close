// Package security retains durable request restriction episodes and scoped audit evidence.
package security

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sh2001sh/new-api/v3/internal/gateway"
)

type Config struct {
	Enabled bool
	Now     func() time.Time
}
type Guard struct {
	pool  *pgxpool.Pool
	redis redis.UniversalClient
	cfg   Config
}
type State struct {
	UserID          int64  `json:"user_id"`
	Strikes         int    `json:"strikes"`
	RestrictedUntil int64  `json:"restricted_until"`
	LastWindowEnd   int64  `json:"last_window_end"`
	Blocked         bool   `json:"blocked"`
	Evidence        string `json:"-"`
	UpdatedAt       int64  `json:"updated_at"`
}

func New(pool *pgxpool.Pool, client redis.UniversalClient, cfg Config) (*Guard, error) {
	if cfg.Enabled && (pool == nil || client == nil) {
		return nil, errors.New("security: enabled guard requires PostgreSQL and Redis")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Guard{pool: pool, redis: client, cfg: cfg}, nil
}
func stateKey(user int64) string { return fmt.Sprintf("v3:request-abuse:state:%d", user) }
func guardError(status int, code, message string) error {
	return &gateway.UpstreamError{Status: status, Type: "permission_error", Code: code, Message: message}
}
func unavailable() error {
	return guardError(503, "account_request_guard_unavailable", "account request protection is temporarily unavailable")
}

var rpmScript = redis.NewScript(`
local now=redis.call('TIME')
local ms=tonumber(now[1])*1000+math.floor(tonumber(now[2])/1000)
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ms-60000)
if redis.call('ZCARD',KEYS[1])>=10 then return 0 end
redis.call('ZADD',KEYS[1],ms,ARGV[1])
redis.call('PEXPIRE',KEYS[1],60000)
return 1`)

// Check runs once per incoming request, across all API keys belonging to a user.
func (g *Guard) Check(parent context.Context, user int64, requestID string) error {
	if !g.cfg.Enabled || user <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	state, err := g.load(ctx, user)
	if err != nil {
		return unavailable()
	}
	if state.Blocked {
		return guardError(403, "account_request_disabled", "account requests are disabled")
	}
	if state.RestrictedUntil <= g.cfg.Now().Unix() {
		return nil
	}
	// A unique admission member prevents callers from defeating the limit by replaying an ID.
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return unavailable()
	}
	allowed, err := rpmScript.Run(ctx, g.redis, []string{fmt.Sprintf("v3:request-abuse:rpm:%d", user)}, requestID+":"+hex.EncodeToString(nonce[:])).Int()
	if err != nil {
		return unavailable()
	}
	if allowed != 1 {
		return guardError(429, "account_request_rpm_reached", "account request rate limit reached")
	}
	return nil
}
func readState(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, user int64) (State, error) {
	var s State
	err := q.QueryRow(ctx, `SELECT user_id,strikes,restricted_until,last_window_end,blocked,coalesce(evidence,''),coalesce(updated_at,0) FROM v3_security.account_request_abuse_states WHERE user_id=$1`, user).Scan(&s.UserID, &s.Strikes, &s.RestrictedUntil, &s.LastWindowEnd, &s.Blocked, &s.Evidence, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return State{UserID: user}, nil
	}
	return s, err
}
func (g *Guard) load(ctx context.Context, user int64) (State, error) {
	value, err := g.redis.Get(ctx, stateKey(user)).Bytes()
	if errors.Is(err, redis.Nil) {
		s, e := readState(ctx, g.pool, user)
		if e != nil {
			return State{}, e
		}
		encoded, e := json.Marshal(s)
		if e != nil {
			return State{}, e
		}
		if e = g.redis.SetNX(ctx, stateKey(user), encoded, time.Minute).Err(); e != nil {
			return State{}, e
		}
		value, err = g.redis.Get(ctx, stateKey(user)).Bytes()
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err = json.Unmarshal(value, &s); err != nil {
		return State{}, err
	}
	if s.UserID != user || s.Strikes < 0 || s.RestrictedUntil < 0 {
		return State{}, errors.New("security: invalid restriction cache")
	}
	return s, nil
}
