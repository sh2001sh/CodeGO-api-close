package desktop

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type StartInput struct {
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}
type StartResult struct {
	SessionID       string `json:"session_id"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int    `json:"interval"`
}
type AuthView struct {
	SessionID   string   `json:"session_id"`
	UserCode    string   `json:"user_code"`
	DeviceName  string   `json:"device_name"`
	Platform    string   `json:"platform"`
	AppVersion  string   `json:"app_version"`
	Status      string   `json:"status"`
	CreatedAt   int64    `json:"created_at"`
	ExpiresAt   int64    `json:"expires_at"`
	ApprovedAt  int64    `json:"approved_at"`
	Permissions []string `json:"permissions"`
}
type PollResult struct {
	Status        string   `json:"status"`
	Authenticated bool     `json:"authenticated"`
	AccessToken   string   `json:"access_token,omitempty"`
	UserID        int64    `json:"user_id,omitempty"`
	DeviceID      int64    `json:"device_id,omitempty"`
	ServerAddress string   `json:"server_address,omitempty"`
	LastUsername  string   `json:"last_username,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
}

func (s *Service) Start(ctx context.Context, in StartInput) (StartResult, error) {
	in.DeviceName = strings.TrimSpace(in.DeviceName)
	if in.DeviceName == "" {
		in.DeviceName = "Code Go Desktop"
	}
	if len(in.DeviceName) > 100 || len(in.Platform) > 64 || len(in.AppVersion) > 64 {
		return StartResult{}, ErrInvalid
	}
	if s.cfg.Crypto == nil {
		return StartResult{}, ErrDenied
	}
	id, err := token("")
	if err != nil {
		return StartResult{}, err
	}
	code, err := token("")
	if err != nil {
		return StartResult{}, err
	}
	code = strings.ToUpper(code[:8])
	_, err = s.pool.Exec(ctx, `INSERT INTO v3_identity.desktop_auth_sessions(session_hash,user_code,device_name,platform,app_version,expires_at)
	 VALUES($1,$2,$3,$4,$5,$6)`, digest(id), code, in.DeviceName, in.Platform, in.AppVersion, s.cfg.Now().Add(10*time.Minute))
	return StartResult{id, code, s.cfg.PublicURL + "/desktop/authorize?session_id=" + id + "&code=" + code, 600, 5}, err
}

func (s *Service) View(ctx context.Context, id, code string) (AuthView, error) {
	var out AuthView
	err := s.pool.QueryRow(ctx, `SELECT user_code,device_name,platform,app_version,
	 CASE WHEN status='pending' AND expires_at<=$3 THEN 'expired' ELSE status END,
	 extract(epoch FROM created_at)::bigint,extract(epoch FROM expires_at)::bigint,coalesce(extract(epoch FROM approved_at)::bigint,0)
	 FROM v3_identity.desktop_auth_sessions WHERE session_hash=$1 AND user_code=$2`, digest(id), strings.ToUpper(strings.TrimSpace(code)), s.cfg.Now()).
		Scan(&out.UserCode, &out.DeviceName, &out.Platform, &out.AppVersion, &out.Status, &out.CreatedAt, &out.ExpiresAt, &out.ApprovedAt)
	out.SessionID = id
	out.Permissions = append([]string{}, authPermissions...)
	return out, dbError(err)
}

// Decide locks the authorization once, preventing two concurrent browser
// approvals from issuing different device grants for the same session.
func (s *Service) Decide(ctx context.Context, uid int64, id string, approve bool) (map[string]any, error) {
	out := map[string]any{}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var in StartInput
		var status string
		var expiry time.Time
		if err := tx.QueryRow(ctx, `SELECT device_name,platform,app_version,status,expires_at FROM v3_identity.desktop_auth_sessions WHERE session_hash=$1 FOR UPDATE`, digest(id)).Scan(&in.DeviceName, &in.Platform, &in.AppVersion, &status, &expiry); err != nil {
			return dbError(err)
		}
		now := s.cfg.Now()
		if status != "pending" || !expiry.After(now) {
			return ErrDenied
		}
		var active bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM v3_identity.users WHERE id=$1 AND status='active' AND deleted_at IS NULL)`, uid).Scan(&active); err != nil {
			return err
		}
		if !active {
			return ErrDenied
		}
		status = "rejected"
		var deviceID any
		if approve {
			if s.cfg.Crypto == nil {
				return ErrDenied
			}
			raw, err := token("desktop_")
			if err != nil {
				return err
			}
			sealed, err := s.cfg.Crypto.Encrypt([]byte(raw))
			if err != nil {
				return err
			}
			var did int64
			err = tx.QueryRow(ctx, `INSERT INTO v3_identity.desktop_devices(user_id,device_name,platform,app_version,token_hash,token_ciphertext,scopes,expires_at)
			 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, uid, in.DeviceName, in.Platform, in.AppVersion, digest(raw), sealed, defaultScopes, now.Add(7*24*time.Hour)).Scan(&did)
			if err != nil {
				return err
			}
			deviceID = did
			status = "approved"
			out["device_id"] = did
			out["device_name"] = in.DeviceName
			out["scopes"] = defaultScopes
			out["expires_at"] = now.Add(7 * 24 * time.Hour).Unix()
		}
		_, err := tx.Exec(ctx, `UPDATE v3_identity.desktop_auth_sessions SET user_id=$2,device_id=$3,status=$4,approved_at=$5 WHERE session_hash=$1`, digest(id), uid, deviceID, status, now)
		out["status"] = status
		out["approved_at"] = now.Unix()
		return err
	})
	return out, err
}

func (s *Service) Poll(ctx context.Context, id string) (PollResult, error) {
	var out PollResult
	var sealed []byte
	err := s.pool.QueryRow(ctx, `SELECT CASE WHEN a.expires_at<=$2 THEN 'expired'
	 WHEN a.status='approved' AND (d.revoked_at IS NOT NULL OR d.expires_at<=$2 OR u.status<>'active' OR u.deleted_at IS NOT NULL) THEN 'rejected' ELSE a.status END,
	 coalesce(a.user_id,0),coalesce(a.device_id,0),coalesce(d.token_ciphertext,''::bytea),coalesce(u.username,''),coalesce(d.scopes,ARRAY[]::text[])
	 FROM v3_identity.desktop_auth_sessions a LEFT JOIN v3_identity.desktop_devices d ON d.id=a.device_id
	 LEFT JOIN v3_identity.users u ON u.id=a.user_id WHERE a.session_hash=$1`, digest(id), s.cfg.Now()).Scan(&out.Status, &out.UserID, &out.DeviceID, &sealed, &out.LastUsername, &out.Scopes)
	if err != nil {
		return out, dbError(err)
	}
	if out.Status == "approved" {
		if s.cfg.Crypto == nil {
			return PollResult{}, ErrDenied
		}
		raw, err := s.cfg.Crypto.Decrypt(sealed)
		if err != nil {
			return PollResult{}, err
		}
		out.AccessToken = string(raw)
		out.Authenticated = true
		out.ServerAddress = s.cfg.PublicURL
	} else {
		out.UserID = 0
		out.DeviceID = 0
		out.LastUsername = ""
		out.Scopes = nil
	}
	return out, nil
}
