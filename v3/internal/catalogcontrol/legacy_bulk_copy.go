package catalogcontrol

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
)

var errLegacyCopyNameTooLong = errors.New("catalogcontrol: copied channel name too long")

// legacyCopySuffix resolves and validates the suffix query parameter used to
// name the copied channel, and validates reset_balance if present.
func legacyCopySuffix(r *http.Request) (suffix string, errCode, errMsg string) {
	suffix = "_复制"
	if r.URL.Query().Has("suffix") {
		suffix = r.URL.Query().Get("suffix")
	}
	if len(suffix) > 255 {
		return "", "invalid_suffix", "Copied channel names must fit within 255 bytes"
	}
	if raw := r.URL.Query().Get("reset_balance"); raw != "" {
		if _, err := strconv.ParseBool(raw); err != nil {
			return "", "invalid_reset_balance", "Expected a boolean reset_balance value"
		}
	}
	return suffix, "", ""
}

// copyChannelWithinTx clones the channel row named by id (appending suffix
// to its name) along with its group/model memberships and credential
// ciphertext, all within tx. No secret is ever decrypted.
func copyChannelWithinTx(ctx context.Context, tx pgx.Tx, id int64, suffix string) (int64, error) {
	var name string
	if err := tx.QueryRow(ctx, `SELECT name FROM v3_catalog.channels WHERE id=$1 FOR SHARE`, id).Scan(&name); err != nil {
		return 0, err
	}
	if len(name+suffix) > 255 {
		return 0, errLegacyCopyNameTooLong
	}
	var copiedID int64
	err := tx.QueryRow(ctx, `INSERT INTO v3_catalog.channels(name,provider,base_url,proxy_url,status,scope,owner_user_id,
	 priority,weight,max_concurrency,max_user_concurrency,auto_disable,multiplier_card_supported,
	 model_mapping,param_override,header_override,status_code_mapping,settings,tag,remark)
	 SELECT name||$2,provider,base_url,proxy_url,status,scope,owner_user_id,
	 priority,weight,max_concurrency,max_user_concurrency,auto_disable,multiplier_card_supported,
	 model_mapping,param_override,header_override,status_code_mapping,settings,tag,remark
	 FROM v3_catalog.channels WHERE id=$1 RETURNING id`, id, suffix).Scan(&copiedID)
	if err != nil {
		return 0, err
	}
	for _, sql := range []string{
		`INSERT INTO v3_catalog.channel_groups(channel_id,group_name) SELECT $2,group_name FROM v3_catalog.channel_groups WHERE channel_id=$1`,
		`INSERT INTO v3_catalog.channel_models(channel_id,model) SELECT $2,model FROM v3_catalog.channel_models WHERE channel_id=$1`,
		// Copy the ciphertext inside PostgreSQL; no secret is decrypted or exposed
		// to the response, and expiry/status/fingerprint limits remain intact.
		`INSERT INTO v3_catalog.channel_credentials(channel_id,kind,secret,status,expires_at,max_concurrency,fingerprint)
		 SELECT $2,kind,secret,status,expires_at,max_concurrency,fingerprint FROM v3_catalog.channel_credentials WHERE channel_id=$1`,
	} {
		if _, err = tx.Exec(ctx, sql, id, copiedID); err != nil {
			return 0, err
		}
	}
	return copiedID, nil
}

func (s *Server) legacyCopyChannel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	suffix, errCode, errMsg := legacyCopySuffix(r)
	if errCode != "" {
		fail(w, 400, errCode, errMsg)
		return
	}
	// Balances and usage are separate records in v3 and are never cloned. A
	// repeatable-read transaction keeps channel settings and credentials coherent.
	tx, err := s.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	copiedID, err := copyChannelWithinTx(r.Context(), tx, id, suffix)
	if err != nil {
		if errors.Is(err, errLegacyCopyNameTooLong) {
			fail(w, 400, "invalid_name", "Copied channel names must fit within 255 bytes")
		} else {
			s.dbError(w, err)
		}
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.dbError(w, err)
		return
	}
	respond(w, 200, map[string]any{"id": copiedID})
}
