package community

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RatingRelay struct {
	pool     *pgxpool.Pool
	endpoint string
	secret   string
	client   *http.Client
	log      *slog.Logger
	now      func() time.Time
}

func NewRatingRelay(pool *pgxpool.Pool, endpoint, secret string, log *slog.Logger) (*RatingRelay, error) {
	endpoint, secret = strings.TrimSpace(endpoint), strings.TrimSpace(secret)
	if log == nil {
		log = slog.Default()
	}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(endpoint, "#") || u.Opaque != "" ||
			(u.Scheme != "https" && (u.Scheme != "http" || !trustedRatingRelayHost(u.Hostname()))) || len(secret) < 32 {
			return nil, errors.New("community rating relay requires HTTPS or a trusted internal endpoint and dedicated service secret")
		}
	}
	return &RatingRelay{pool: pool, endpoint: endpoint, secret: secret, log: log, now: time.Now,
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func trustedRatingRelayHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "host.docker.internal", "nodebb", "codego-community":
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && (ip.IsLoopback() || ip.IsPrivate())
}

// Run uses PostgreSQL change notifications for wake-up and only schedules a
// retry when a durable event is pending. An idle community receives no polling.
func (r *RatingRelay) Run(ctx context.Context) error {
	if r.endpoint == "" {
		r.log.Warn("community rating sync is not configured; rating events remain queued")
		<-ctx.Done()
		return ctx.Err()
	}
	if r.pool == nil {
		return ErrUnavailable
	}
	for ctx.Err() == nil {
		err := r.listen(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r.log.Error("community rating event listener failed; reconnecting", "err", err)
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return ctx.Err()
}

func (r *RatingRelay) listen(ctx context.Context) error {
	// Dedicated connection: LISTEN state must never leak to another pool user.
	conn, err := pgx.ConnectConfig(ctx, r.pool.Config().ConnConfig.Copy())
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()
	if _, err = conn.Exec(ctx, "LISTEN v3_market_rating_changed"); err != nil {
		return err
	}
	for ctx.Err() == nil {
		if err = r.DeliverPending(ctx); err != nil {
			return err
		}
		var next *time.Time
		if err = r.pool.QueryRow(ctx, `SELECT min(available_at) FROM v3_community.rating_event_outbox WHERE delivered_at IS NULL`).Scan(&next); err != nil {
			return err
		}
		waitCtx := ctx
		cancel := func() {}
		if next != nil {
			deadline := *next
			if deadline.Before(time.Now().Add(100 * time.Millisecond)) {
				deadline = time.Now().Add(100 * time.Millisecond)
			}
			waitCtx, cancel = context.WithDeadline(ctx, deadline)
		}
		_, err = conn.WaitForNotification(waitCtx)
		cancel()
		if errors.Is(err, context.DeadlineExceeded) {
			continue // only a known queued event causes a timed wake-up
		}
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

type ratingEvent struct {
	ChannelID string `json:"channel_id"`
	OwnerSub  string `json:"owner_sub"`
	Version   string `json:"version"`
}

// DeliverPending commits successful delivery markers or bounded retry delays.
// SKIP LOCKED permits multiple workers; the receiver deduplicates by version.
func (r *RatingRelay) DeliverPending(ctx context.Context) error {
	if r.endpoint == "" {
		return nil // disabled sync keeps every event in the durable outbox
	}
	if r.pool == nil {
		return ErrUnavailable
	}
	for i := 0; i < 100; i++ {
		delivered, err := r.deliverOne(ctx)
		if err != nil || !delivered {
			return err
		}
	}
	return nil
}

func (r *RatingRelay) deliverOne(ctx context.Context) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	var attempts int
	var event ratingEvent
	err = tx.QueryRow(ctx, `SELECT id,channel_id,owner_sub,attempts FROM v3_community.rating_event_outbox
 WHERE delivered_at IS NULL AND available_at<=now() ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &event.ChannelID, &event.OwnerSub, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	event.Version = strconv.FormatInt(id, 10)
	if err = r.send(ctx, event); err != nil {
		r.log.Warn("community rating event delivery failed; retry queued", "event_id", id, "err", err)
		attempts++
		delay := 10 * time.Second
		for j := 1; j < attempts && delay < 5*time.Minute; j++ {
			delay *= 2
		}
		if delay > 5*time.Minute {
			delay = 5 * time.Minute
		}
		_, err = tx.Exec(ctx, `UPDATE v3_community.rating_event_outbox SET attempts=$2,available_at=now()+($3::bigint*interval '1 millisecond') WHERE id=$1`, id, attempts, delay.Milliseconds())
	} else {
		_, err = tx.Exec(ctx, `UPDATE v3_community.rating_event_outbox SET delivered_at=now() WHERE id=$1`, id)
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *RatingRelay) send(ctx context.Context, event ratingEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/vnd.codego.rating-event+json")
	req.Header.Set("Authorization", "Bearer "+r.secret)
	timestamp := strconv.FormatInt(r.now().Unix(), 10)
	signature := hmac.New(sha256.New, []byte(r.secret))
	_, _ = signature.Write([]byte(timestamp + "."))
	_, _ = signature.Write(body)
	req.Header.Set("X-CodeGo-Timestamp", timestamp)
	req.Header.Set("X-CodeGo-Signature", hex.EncodeToString(signature.Sum(nil)))
	res, err := r.client.Do(req)
	if err != nil {
		return errors.New("community rating event request failed") // endpoint and credentials stay out of logs
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("community rating event returned HTTP %d", res.StatusCode)
	}
	ack, err := io.ReadAll(io.LimitReader(res.Body, 4097))
	if err != nil || len(ack) > 4096 {
		return errors.New("community rating event acknowledgement is invalid")
	}
	var result struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(ack, &result); err != nil || !result.Success {
		return errors.New("community rating event was not acknowledged")
	}
	return nil
}
