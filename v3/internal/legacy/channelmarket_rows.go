package legacy

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sh2001sh/new-api/v3/internal/catalog"
)

// All identifiers below are fixed schema names, never supplied by source rows.
var channelMarketSourceTables = []string{
	"channels", "groups", "channel_id_sequences", "group_invites", "group_access",
	"channel_user_blocks", "user_multipliers", "time_range_multipliers", "multiplier_notices",
	"bargain_requests", "route_pools", "route_pool_members", "auto_route_pool_configs",
	"auto_route_pool_members", "settlements", "income_reclaims", "verification_runs",
	"gpt56_mapping_runs", "ranking_snapshots", "multiplier_trend_snapshots", "channel_feedback", "pelican_artifacts",
}

type cmRow map[string]json.RawMessage

func (r cmRow) text(key string) string {
	v := r[key]
	if len(v) == 0 || string(v) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) == nil {
		return s
	}
	return string(v)
}
func (r cmRow) integer(key string) (int64, error) {
	s := r.text(key)
	if s == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s integer", key)
	}
	return v, nil
}
func (r cmRow) timestamp(key string) (any, error) {
	s := r.text(key)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil, fmt.Errorf("invalid %s timestamp", key)
	}
	return t.UTC(), nil
}
func (r cmRow) structured(key, fallback string) (json.RawMessage, error) {
	s := r.text(key)
	if s == "" {
		s = fallback
	}
	if !json.Valid([]byte(s)) {
		return nil, fmt.Errorf("invalid %s JSON", key)
	}
	return json.RawMessage(s), nil
}
func cmFactor(s string, allowZero bool) (int64, error) {
	if s == "" && allowZero {
		return 0, nil
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok || r.Sign() < 0 || (!allowZero && r.Sign() == 0) {
		return 0, errors.New("invalid multiplier")
	}
	r.Mul(r, big.NewRat(1000000, 1))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(r.Num(), r.Denom(), rem)
	// Live factor columns require integral PPM. Historical settlement metadata
	// has a separate exact decimal projection; never round a routing price.
	if rem.Sign() != 0 || !q.IsInt64() {
		return 0, errors.New("multiplier cannot be represented exactly")
	}
	return q.Int64(), nil
}
func cmSecret(value, sourceSecret string) (string, error) {
	if !strings.HasPrefix(value, "enc:v1:") {
		return value, nil
	}
	if sourceSecret == "" {
		return "", errors.New("V3_MIGRATION_SOURCE_CRYPTO_SECRET is required for encrypted market credentials")
	}
	encoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "enc:v1:"))
	if err != nil {
		return "", errors.New("invalid encrypted market credential encoding")
	}
	digest := sha256.Sum256([]byte(sourceSecret))
	crypto, err := catalog.NewAESGCM(digest[:])
	if err != nil {
		return "", err
	}
	plain, err := crypto.Decrypt(encoded)
	if err != nil {
		return "", errors.New("market credential decryption failed; verify source crypto secret")
	}
	return string(plain), nil
}
func cmHex(value string) (string, error) {
	b, err := hex.DecodeString(value)
	if err != nil || len(b) != 32 {
		return "", errors.New("expected SHA256 hex digest")
	}
	return "\\x" + hex.EncodeToString(b), nil
}

type cmRecord struct {
	table  string
	keys   []string
	values map[string]any
}

// Insert typed target columns, retaining natural keys and rejecting altered
// replay. JSON is a transport for PostgreSQL's typed row constructor, not an
// archive of a legacy row; values are explicit native target fields only.
func cmInsert(ctx context.Context, tx pgx.Tx, r cmRecord) error {
	columns := make([]string, 0, len(r.values))
	for k := range r.values {
		columns = append(columns, k)
	}
	sort.Strings(columns)
	quoted := make([]string, len(columns))
	selected := make([]string, len(columns))
	same := make([]string, len(columns))
	for i, c := range columns {
		q := pgx.Identifier{c}.Sanitize()
		quoted[i] = q
		selected[i] = "r." + q
		same[i] = "t." + q + " IS NOT DISTINCT FROM r." + q
	}
	payload, err := json.Marshal(r.values)
	if err != nil {
		return err
	}
	query := `INSERT INTO ` + r.table + ` (` + strings.Join(quoted, ",") + `) SELECT ` + strings.Join(selected, ",") + ` FROM jsonb_populate_record(NULL::` + r.table + `,$1::jsonb) r ON CONFLICT DO NOTHING`
	tag, err := tx.Exec(ctx, query, payload)
	if err != nil {
		return fmt.Errorf("legacy: market insert %s: %w", r.table, err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	keyMatch := make([]string, len(r.keys))
	for i, k := range r.keys {
		q := pgx.Identifier{k}.Sanitize()
		keyMatch[i] = "t." + q + " IS NOT DISTINCT FROM r." + q
	}
	var equal bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+r.table+` t CROSS JOIN jsonb_populate_record(NULL::`+r.table+`,$1::jsonb) r WHERE `+strings.Join(keyMatch, " AND ")+` AND `+strings.Join(same, " AND ")+`)`, payload).Scan(&equal)
	if err != nil {
		return err
	}
	if !equal {
		return fmt.Errorf("legacy: target market collision or changed replay in %s", r.table)
	}
	return nil
}

type cmBuilder struct {
	values map[string]any
	err    error
}

func (b *cmBuilder) put(name string, value any) { b.values[name] = value }
func (b *cmBuilder) integer(r cmRow, source, target string) int64 {
	v, err := r.integer(source)
	if err != nil && b.err == nil {
		b.err = err
	}
	b.put(target, v)
	return v
}
func (b *cmBuilder) money(r cmRow, source, target string) int64 {
	v, err := r.integer(source)
	if err == nil {
		m, e := FromV2Units(v)
		v = int64(m)
		err = e
	}
	if err == nil && v < 0 {
		err = fmt.Errorf("negative %s", source)
	}
	if err != nil && b.err == nil {
		b.err = err
	}
	b.put(target, v)
	return v
}
func (b *cmBuilder) factor(r cmRow, source, target string, allowZero bool) int64 {
	v, err := cmFactor(r.text(source), allowZero)
	if err != nil && b.err == nil {
		b.err = fmt.Errorf("%s: %w", source, err)
	}
	b.put(target, v)
	return v
}
func (b *cmBuilder) times(r cmRow, names ...string) {
	for _, name := range names {
		v, err := r.timestamp(name)
		if err != nil && b.err == nil {
			b.err = err
		}
		if v != nil {
			b.put(name, v)
		}
	}
}
func (b *cmBuilder) texts(r cmRow, names ...string) {
	for _, name := range names {
		b.put(name, r.text(name))
	}
}
func (b *cmBuilder) json(r cmRow, source, target, fallback string) {
	v, err := r.structured(source, fallback)
	if err != nil && b.err == nil {
		b.err = err
	}
	b.put(target, v)
}
func cmBuild() *cmBuilder { return &cmBuilder{values: map[string]any{}} }
