package channelmarket

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Invite struct {
	ID        int64     `json:"id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Service) CreateInvite(ctx context.Context, a Actor, channel int64, expiry time.Time) (Invite, error) {
	var result Invite
	if expiry.IsZero() {
		expiry = s.cfg.Now().Add(7 * 24 * time.Hour)
	}
	if !expiry.After(s.cfg.Now()) || expiry.After(s.cfg.Now().Add(30*24*time.Hour)) {
		return result, ErrInvalid
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return result, err
	}
	result.Token = base64.RawURLEncoding.EncodeToString(b[:])
	hash := sha256.Sum256([]byte(result.Token))
	result.ExpiresAt = expiry
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		id, err := owned(ctx, tx, a, channel)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `INSERT INTO v3_channelmarket.group_invites(group_id,created_by,token_hash,expires_at) VALUES($1,$2,$3,$4) RETURNING id`, id, a.UserID, hash[:], expiry).Scan(&result.ID)
	})
	return result, err
}

func (s *Service) AcceptInvite(ctx context.Context, user int64, token string) (string, error) {
	var group string
	if user <= 0 || len(token) < 32 || len(token) > 128 {
		return group, ErrInvalid
	}
	hash := sha256.Sum256([]byte(token))
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		var invite, channel int64
		err := tx.QueryRow(ctx, `SELECT i.id,i.group_id,g.channel_id FROM v3_channelmarket.group_invites i JOIN v3_channelmarket.groups g ON g.id=i.group_id WHERE i.token_hash=$1 AND i.revoked_at IS NULL AND (i.expires_at IS NULL OR i.expires_at>$2) AND g.deleted_at IS NULL FOR UPDATE OF i`, hash[:], s.cfg.Now()).Scan(&invite, &group, &channel)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var blocked bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_channelmarket.channel_user_blocks WHERE channel_id=$1 AND user_id=$2)`, channel, user).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `INSERT INTO v3_channelmarket.group_access(group_id,user_id,invite_id) VALUES($1,$2,$3) ON CONFLICT(group_id,user_id) DO NOTHING`, group, user, invite)
		return err
	})
	return group, err
}

func (s *Service) BindToken(ctx context.Context, user int64, group string, key int64) error {
	if user <= 0 || key <= 0 {
		return ErrInvalid
	}
	return s.transaction(ctx, func(tx pgx.Tx) error {
		if err := accessible(ctx, tx, user, group); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE v3_identity.api_keys SET group_name=(SELECT internal_group_name FROM v3_channelmarket.groups WHERE id=$1) WHERE id=$2 AND user_id=$3 AND status='active'`, group, key, user)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *Service) SetBlock(ctx context.Context, a Actor, channel, user int64, blocked bool) error {
	if user <= 0 || user == a.UserID {
		return ErrInvalid
	}
	return s.transaction(ctx, func(tx pgx.Tx) error {
		if _, err := owned(ctx, tx, a, channel); err != nil {
			return err
		}
		if blocked {
			_, err := tx.Exec(ctx, `INSERT INTO v3_channelmarket.channel_user_blocks(channel_id,user_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, channel, user)
			if err != nil {
				return err
			}
			return securityTx(ctx, tx, a, channel, "consumer_block", map[string]any{"user_id": user, "blocked": true})
		}
		_, err := tx.Exec(ctx, `DELETE FROM v3_channelmarket.channel_user_blocks WHERE channel_id=$1 AND user_id=$2`, channel, user)
		if err != nil {
			return err
		}
		return securityTx(ctx, tx, a, channel, "consumer_block", map[string]any{"user_id": user, "blocked": false})
	})
}

type Block struct {
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"blocked_at"`
}

func (s *Service) Blocks(ctx context.Context, a Actor, channel int64) ([]Block, error) {
	result := []Block{}
	err := s.transaction(ctx, func(tx pgx.Tx) error {
		if _, e := owned(ctx, tx, a, channel); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT b.user_id,u.username,b.created_at FROM v3_channelmarket.channel_user_blocks b JOIN v3_identity.users u ON u.id=b.user_id WHERE channel_id=$1 ORDER BY b.user_id LIMIT 1000`, channel)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b Block
			if e = rows.Scan(&b.UserID, &b.Username, &b.CreatedAt); e != nil {
				return e
			}
			result = append(result, b)
		}
		return rows.Err()
	})
	return result, err
}
