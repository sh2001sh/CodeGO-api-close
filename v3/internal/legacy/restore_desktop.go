package legacy

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

var restoredDesktopScopes = []string{"account:read", "logs:read", "tokens:read", "tokens:write", "config:read", "config:write", "telemetry:write"}

func projectRestoredDevice(raw json.RawMessage) (restoredRecord, error) {
	var old struct {
		ID          int64  `json:"id"`
		UserID      int64  `json:"user_id"`
		DeviceName  string `json:"device_name"`
		Platform    string `json:"platform"`
		AppVersion  string `json:"app_version"`
		AccessToken string `json:"access_token"`
		Scopes      string `json:"scopes"`
		Status      string `json:"status"`
		CreatedAt   int64  `json:"created_at"`
		LastUsedAt  int64  `json:"last_used_at"`
		ExpiresAt   int64  `json:"expires_at"`
		RevokedAt   int64  `json:"revoked_at"`
	}
	r := restoredRecord{table: "v3_identity.desktop_devices", key: "id", secretColumn: "token_ciphertext"}
	if err := securityRequiredFields(raw, "id", "user_id", "device_name", "access_token", "status", "created_at", "expires_at"); err != nil {
		return r, err
	}
	if json.Unmarshal(raw, &old) != nil {
		return r, fmt.Errorf("invalid desktop device fields")
	}
	r.id = old.ID
	if old.ID <= 0 || old.UserID <= 0 || old.DeviceName == "" || len(old.DeviceName) > 128 || len(old.Platform) > 64 || len(old.AppVersion) > 64 {
		return r, fmt.Errorf("invalid desktop device identifiers or metadata")
	}
	if !strings.HasPrefix(old.AccessToken, "desktop_") || len(old.AccessToken) < 16 || len(old.AccessToken) > 128 {
		return r, fmt.Errorf("invalid desktop device credential")
	}
	if old.Status != "active" && old.Status != "revoked" {
		return r, fmt.Errorf("invalid desktop device status")
	}
	for _, timestamp := range []int64{old.CreatedAt, old.LastUsedAt, old.ExpiresAt, old.RevokedAt} {
		if timestamp < 0 || timestamp > 253402300799 {
			return r, fmt.Errorf("invalid desktop timestamp")
		}
	}
	scopes, err := projectRestoredScopes(old.Scopes)
	if err != nil {
		return r, err
	}
	expires := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	if old.ExpiresAt > 0 {
		expires = time.Unix(old.ExpiresAt, 0).UTC()
	}
	var revoked any
	if old.Status == "revoked" || old.RevokedAt > 0 {
		revoked = time.Unix(old.RevokedAt, 0).UTC()
	}
	hash := sha256.Sum256([]byte(old.AccessToken))
	r.secret = old.AccessToken
	r.fields = map[string]any{"id": old.ID, "user_id": old.UserID, "device_name": old.DeviceName, "platform": old.Platform, "app_version": old.AppVersion, "token_hash": hash[:], "scopes": scopes, "created_at": time.Unix(old.CreatedAt, 0).UTC(), "last_used_at": time.Unix(old.LastUsedAt, 0).UTC(), "expires_at": expires, "revoked_at": revoked}
	return r, nil
}

func projectRestoredScopes(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return append([]string{}, restoredDesktopScopes...), nil
	}
	allowed := map[string]bool{}
	for _, scope := range restoredDesktopScopes {
		allowed[scope] = true
	}
	seen := map[string]bool{}
	for _, scope := range strings.Split(raw, ",") {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if !strings.HasPrefix(scope, "desktop:") {
			return nil, fmt.Errorf("unknown legacy desktop scope")
		}
		scope = strings.TrimPrefix(scope, "desktop:")
		if !allowed[scope] {
			return nil, fmt.Errorf("unknown legacy desktop scope")
		}
		seen[scope] = true
	}
	if len(seen) == 0 {
		return append([]string{}, restoredDesktopScopes...), nil
	}
	if seen["config:write"] {
		seen["telemetry:write"] = true
	}
	out := make([]string, 0, len(seen))
	for scope := range seen {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out, nil
}

func projectRestoredSession(raw json.RawMessage) (restoredRecord, error) {
	var old struct {
		SessionID  string `json:"session_id"`
		UserCode   string `json:"user_code"`
		UserID     int64  `json:"user_id"`
		DeviceID   int64  `json:"device_id"`
		DeviceName string `json:"device_name"`
		Platform   string `json:"platform"`
		AppVersion string `json:"app_version"`
		Status     string `json:"status"`
		CreatedAt  int64  `json:"created_at"`
		ApprovedAt int64  `json:"approved_at"`
		ExpiresAt  int64  `json:"expires_at"`
	}
	r := restoredRecord{table: "v3_identity.desktop_auth_sessions", key: "session_hash"}
	if err := securityRequiredFields(raw, "session_id", "user_code", "device_name", "status", "created_at", "expires_at"); err != nil {
		return r, err
	}
	if json.Unmarshal(raw, &old) != nil {
		return r, fmt.Errorf("invalid desktop authorization session")
	}
	if old.SessionID == "" || len(old.SessionID) > 64 || old.UserCode == "" || len(old.UserCode) > 16 || old.DeviceName == "" || len(old.DeviceName) > 128 || len(old.Platform) > 64 || len(old.AppVersion) > 64 {
		return r, fmt.Errorf("invalid desktop session metadata")
	}
	if old.CreatedAt < 0 || old.ExpiresAt <= old.CreatedAt || old.ExpiresAt > 253402300799 || old.ApprovedAt < 0 || old.ApprovedAt > 253402300799 || old.UserID < 0 || old.DeviceID < 0 {
		return r, fmt.Errorf("invalid desktop session references or timestamps")
	}
	switch old.Status {
	case "pending", "approved", "rejected", "expired":
	default:
		return r, fmt.Errorf("invalid desktop session status")
	}
	if old.Status == "approved" && (old.UserID == 0 || old.DeviceID == 0 || old.ApprovedAt == 0) {
		return r, fmt.Errorf("approved desktop session lacks its grant")
	}
	var user, device, approved any
	if old.UserID > 0 {
		user = old.UserID
	}
	if old.DeviceID > 0 {
		device = old.DeviceID
	}
	if old.ApprovedAt > 0 {
		approved = time.Unix(old.ApprovedAt, 0).UTC()
	}
	hash := sha256.Sum256([]byte(old.SessionID))
	r.fields = map[string]any{"session_hash": hash[:], "user_code": old.UserCode, "user_id": user, "device_id": device, "device_name": old.DeviceName, "platform": old.Platform, "app_version": old.AppVersion, "status": old.Status, "created_at": time.Unix(old.CreatedAt, 0).UTC(), "expires_at": time.Unix(old.ExpiresAt, 0).UTC(), "approved_at": approved}
	return r, nil
}
