package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) templateHTTP(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.device(w, r, "config:read"); !ok {
		return
	}
	if strings.HasSuffix(r.URL.Path, "/template") {
		out, err := s.template(r.URL.Query().Get("tool"))
		reply(w, out, err)
		return
	}
	out := map[string]Template{}
	for _, tool := range tools {
		t, _ := s.template(tool)
		out[tool] = t
	}
	reply(w, map[string]any{"base_url": s.cfg.PublicURL + "/v1", "tools": out}, nil)
}

func (s *Service) configHTTP(w http.ResponseWriter, r *http.Request) {
	d, ok := s.device(w, r, "tokens:read")
	if !ok {
		return
	}
	id := positiveID(r.PathValue("id"))
	k, err := s.loadKey(r.Context(), d.UserID, id)
	if err != nil {
		reply(w, nil, err)
		return
	}
	if k.Status != "active" || k.ExpiresAt != nil && !k.ExpiresAt.After(s.cfg.Now()) {
		reply(w, nil, ErrDenied)
		return
	}
	raw, err := s.id.RevealKey(r.Context(), d.UserID, id)
	if err != nil {
		reply(w, nil, err)
		return
	}
	models, err := s.keyModels(r, d.UserID, k)
	if err != nil {
		reply(w, nil, err)
		return
	}
	out := map[string]ImportPayload{}
	for _, tool := range tools {
		payload, err := s.buildConfig(raw, recommendedInput(tool, models))
		if err != nil {
			reply(w, nil, err)
			return
		}
		out[tool] = payload
	}
	dto, err := keyDTO(r, k)
	reply(w, map[string]any{"token": dto, "server_address": s.cfg.PublicURL, "tools": out}, err)
}

func (s *Service) CreateImport(ctx context.Context, uid int64, in ImportInput) (map[string]any, error) {
	target := strings.ToLower(strings.TrimSpace(in.Target))
	if target == "" {
		target = "codego"
	}
	if target != "codego" && target != "ccswitch" {
		return nil, ErrInvalid
	}
	k, err := s.loadKey(ctx, uid, in.TokenID)
	if err != nil {
		return nil, err
	}
	if k.Status != "active" || k.ExpiresAt != nil && !k.ExpiresAt.After(s.cfg.Now()) {
		return nil, ErrDenied
	}
	raw, err := s.id.RevealKey(ctx, uid, in.TokenID)
	if err != nil {
		return nil, err
	}
	payload, err := s.buildConfig(raw, in)
	if err != nil {
		return nil, err
	}
	params := url.Values{"resource": {"provider"}, "app": {payload.Tool}, "name": {payload.Name}, "endpoint": {payload.Endpoint}, "homepage": {payload.Homepage}, "enabled": {strconv.FormatBool(payload.Enabled)}, "icon": {payload.Icon}}
	var code, configURL string
	var expires int64
	if target == "codego" {
		if s.cfg.Crypto == nil {
			return nil, ErrDenied
		}
		code, err = token("")
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		sealed, err := s.cfg.Crypto.Encrypt(b)
		if err != nil {
			return nil, err
		}
		_, err = s.pool.Exec(ctx, `INSERT INTO v3_identity.desktop_imports(code_hash,user_id,key_id,ciphertext,expires_at) VALUES($1,$2,$3,$4,$5)`, digest(code), uid, k.ID, sealed, s.cfg.Now().Add(10*time.Minute))
		if err != nil {
			return nil, err
		}
		configURL = s.cfg.PublicURL + "/api/desktop/import/config?code=" + url.QueryEscape(code)
		expires = 600
		params.Set("codegoAction", "applyToolConfig")
		params.Set("configUrl", configURL)
		params.Set("configFormat", payload.ConfigFormat)
		params.Set("tokenId", strconv.FormatInt(k.ID, 10))
	} else {
		params.Set("apiKey", raw)
		params.Set("usageEnabled", "true")
		params.Set("usageAutoInterval", "10")
	}
	for key, value := range map[string]string{"model": payload.Model, "haikuModel": payload.HaikuModel, "sonnetModel": payload.SonnetModel, "opusModel": payload.OpusModel, "notes": payload.Notes} {
		if value != "" {
			params.Set(key, value)
		}
	}
	return map[string]any{"code": code, "deep_link": target + "://v1/import?" + params.Encode(), "config_url": configURL, "expires_in_seconds": expires, "tool": payload.Tool, "token_name": k.Name, "provider_name": payload.Name}, nil
}

// DELETE ... RETURNING consumes the secret once even under concurrent requests.
// Current key/user state is checked at consumption, after a browser creates it.
func (s *Service) ConsumeImport(ctx context.Context, code string) (ImportPayload, error) {
	if len(code) < 32 || len(code) > 100 {
		return ImportPayload{}, ErrInvalid
	}
	if s.cfg.Crypto == nil {
		return ImportPayload{}, ErrDenied
	}
	var out ImportPayload
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var b []byte
		err := tx.QueryRow(ctx, `DELETE FROM v3_identity.desktop_imports i USING v3_identity.api_keys k,v3_identity.users u
		 WHERE i.code_hash=$1 AND i.expires_at>$2 AND k.id=i.key_id AND k.user_id=i.user_id AND k.status='active' AND k.deleted_at IS NULL
		 AND (k.expires_at IS NULL OR k.expires_at>$2) AND u.id=i.user_id AND u.status='active' AND u.deleted_at IS NULL RETURNING i.ciphertext`, digest(code), s.cfg.Now()).Scan(&b)
		if err != nil {
			return dbError(err)
		}
		plain, err := s.cfg.Crypto.Decrypt(b)
		if err != nil {
			return err
		}
		return json.Unmarshal(plain, &out)
	})
	return out, err
}
func (s *Service) importHTTP(w http.ResponseWriter, r *http.Request) {
	u, ok := s.browser(w, r)
	if !ok {
		return
	}
	var in ImportInput
	if err := decode(w, r, &in); err != nil {
		reply(w, nil, err)
		return
	}
	out, err := s.CreateImport(r.Context(), u.ID, in)
	reply(w, out, err)
}
func (s *Service) consumeHTTP(w http.ResponseWriter, r *http.Request) {
	out, err := s.ConsumeImport(r.Context(), r.URL.Query().Get("code"))
	reply(w, out, err)
}
