package catalogcontrol

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

type Setting struct {
	Key        string          `json:"key"`
	Value      json.RawMessage `json:"value,omitempty"`
	Sensitive  bool            `json:"sensitive"`
	Configured bool            `json:"configured"`
}

// sensitiveKey protects existing v2 secrets even if a client omits the flag.
func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, suffix := range []string{"secret", "password", "token", "privatekey", "private_key", "apikey", "api_key"} {
		if strings.Contains(key, suffix) {
			return true
		}
	}
	return false
}

func (s *Server) listSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT key,value,sensitive FROM v3_platform.settings ORDER BY key`)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	items := make([]Setting, 0)
	for rows.Next() {
		var item Setting
		if err = rows.Scan(&item.Key, &item.Value, &item.Sensitive); err != nil {
			s.dbError(w, err)
			return
		}
		item.Configured = true
		if item.Sensitive {
			item.Value = nil
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, items)
}

func (s *Server) saveSetting(w http.ResponseWriter, r *http.Request) {
	var item Setting
	if !decode(w, r, &item) {
		return
	}
	key := r.PathValue("key")
	if item.Key != "" && item.Key != key {
		fail(w, 400, "invalid_key", "Body and path setting keys differ")
		return
	}
	item.Key = key
	s.writeSetting(w, r, item)
}

func (s *Server) saveLegacySetting(w http.ResponseWriter, r *http.Request) {
	var item Setting
	if !decode(w, r, &item) {
		return
	}
	if !json.Valid(item.Value) {
		fail(w, 400, "invalid_setting", "A setting requires a valid JSON value")
		return
	}
	var value string
	if json.Unmarshal(item.Value, &value) != nil {
		value = string(item.Value)
	}
	item.Value, _ = json.Marshal(value)
	s.writeSetting(w, r, item)
}

// Existing clients expect option values as strings and omit secret options.
func (s *Server) legacyListSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT key,value FROM v3_platform.settings WHERE NOT sensitive ORDER BY key`)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer rows.Close()
	type option struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	items := make([]option, 0)
	for rows.Next() {
		var item option
		var raw json.RawMessage
		if err = rows.Scan(&item.Key, &raw); err != nil {
			s.dbError(w, err)
			return
		}
		if json.Unmarshal(raw, &item.Value) != nil {
			item.Value = string(raw)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, items)
}

func (s *Server) writeSetting(w http.ResponseWriter, r *http.Request, item Setting) {
	if item.Key == "" || len(item.Key) > 255 || !json.Valid(item.Value) {
		fail(w, 400, "invalid_setting", "A setting requires a key and a valid JSON value")
		return
	}
	if item.Key == "SubscriptionGroupPolicy" {
		if _, err := catalog.ParseSubscriptionPolicies(item.Value); err != nil {
			fail(w, 400, "invalid_setting", "Invalid subscription group policy")
			return
		}
	}
	var existingSensitive bool
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	// Serialize the first insert too; updates must never downgrade an
	// explicitly sensitive setting into the public gateway snapshot.
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, item.Key); err != nil {
		s.dbError(w, err)
		return
	}
	err = tx.QueryRow(r.Context(), `SELECT sensitive FROM v3_platform.settings WHERE key=$1`, item.Key).Scan(&existingSensitive)
	if err != nil && err != pgx.ErrNoRows {
		s.dbError(w, err)
		return
	}
	item.Sensitive = item.Sensitive || existingSensitive || sensitiveKey(item.Key)
	var ciphertext []byte
	value := item.Value
	if item.Sensitive {
		if s.enc == nil {
			fail(w, 503, "encryption_unavailable", "Configuration encryption is unavailable")
			return
		}
		ciphertext, err = s.enc.Encrypt(item.Value)
		if err != nil {
			s.dbError(w, err)
			return
		}
		value = nil
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO v3_platform.settings(key,value,ciphertext,sensitive) VALUES($1,$2,$3,$4) ON CONFLICT(key) DO UPDATE SET value=excluded.value,ciphertext=excluded.ciphertext,sensitive=excluded.sensitive`, item.Key, value, ciphertext, item.Sensitive)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, nil)
}

func (s *Server) deleteSetting(w http.ResponseWriter, r *http.Request) {
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM v3_platform.settings WHERE key=$1`, r.PathValue("key"))
	if err != nil {
		s.dbError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		s.dbError(w, pgx.ErrNoRows)
		return
	}
	respond(w, 200, nil)
}
