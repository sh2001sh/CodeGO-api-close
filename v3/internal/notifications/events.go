package notifications

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errTooManyStreams = errors.New("notifications: too many streams")

// All subscribers share a single dedicated LISTEN connection per process.
// No database connection or periodic count query is allocated per browser tab.
type eventHub struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	mu     sync.Mutex
	subs   map[int64]map[chan struct{}]struct{}
	cancel context.CancelFunc
}

func newEventHub(pool *pgxpool.Pool, log *slog.Logger) *eventHub {
	return &eventHub{pool: pool, log: log, subs: map[int64]map[chan struct{}]struct{}{}}
}
func (h *eventHub) subscribe(ctx context.Context, user int64) (<-chan struct{}, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pool == nil {
		return nil, nil, ErrUnavailable
	}
	if len(h.subs[user]) >= 4 {
		return nil, nil, errTooManyStreams
	}
	if h.cancel == nil {
		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, err := h.connect(connectCtx)
		cancel()
		if err != nil {
			return nil, nil, err
		}
		listenCtx, stop := context.WithCancel(context.Background())
		h.cancel = stop
		go h.listen(listenCtx, conn)
	}
	c := make(chan struct{}, 1)
	if h.subs[user] == nil {
		h.subs[user] = map[chan struct{}]struct{}{}
	}
	h.subs[user][c] = struct{}{}
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			delete(h.subs[user], c)
			if len(h.subs[user]) == 0 {
				delete(h.subs, user)
			}
			if len(h.subs) == 0 && h.cancel != nil {
				h.cancel()
				h.cancel = nil
			}
		})
	}
	return c, unsubscribe, nil
}
func (h *eventHub) connect(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.ConnectConfig(ctx, h.pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, err
	}
	if _, err = conn.Exec(ctx, `LISTEN v3_notifications`); err != nil {
		_ = conn.Close(ctx)
		return nil, err
	}
	return conn, nil
}
func (h *eventHub) signal(user int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, subs := range h.subs {
		if user != 0 && id != user {
			continue
		}
		for c := range subs {
			select {
			case c <- struct{}{}:
			default:
			}
		}
	}
}
func (h *eventHub) listen(ctx context.Context, conn *pgx.Conn) {
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if conn != nil {
			_ = conn.Close(closeCtx)
		}
	}()
	for ctx.Err() == nil {
		n, err := conn.WaitForNotification(ctx)
		if err == nil {
			if user, e := strconv.ParseInt(n.Payload, 10, 64); e == nil && user > 0 {
				h.signal(user)
			}
			continue
		}
		if ctx.Err() != nil {
			return
		}
		h.log.Warn("notification event listener disconnected", "err", err)
		closeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_ = conn.Close(closeCtx)
		cancel()
		conn = nil
		for ctx.Err() == nil {
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			next, e := h.connect(connectCtx)
			cancel()
			if e != nil {
				h.log.Warn("notification event listener reconnect failed", "err", e)
				continue
			}
			conn = next
			h.signal(0)
			break // reconcile notifications committed while disconnected
		}
	}
}
