// Package settings reads runtime options for control-plane services. Only
// non-sensitive values enter the gateway's catalog snapshot.
package settings

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type Store struct {
	pool *pgxpool.Pool
	dec  catalog.Decrypter
}

func New(pool *pgxpool.Pool, dec catalog.Decrypter) *Store { return &Store{pool: pool, dec: dec} }

func (s *Store) Get(ctx context.Context, key string) (json.RawMessage, error) {
	var value, ciphertext []byte
	var sensitive bool
	if err := s.pool.QueryRow(ctx, `SELECT value,ciphertext,sensitive FROM v3_platform.settings WHERE key=$1`, key).Scan(&value, &ciphertext, &sensitive); err != nil {
		return nil, fmt.Errorf("settings: read: %w", err)
	}
	if sensitive {
		if s.dec == nil {
			return nil, fmt.Errorf("settings: secret decryption is unavailable")
		}
		var err error
		value, err = s.dec.Decrypt(ciphertext)
		if err != nil {
			return nil, fmt.Errorf("settings: decrypt: %w", err)
		}
	}
	if !json.Valid(value) {
		return nil, fmt.Errorf("settings: stored value is invalid JSON")
	}
	return json.RawMessage(value), nil
}
