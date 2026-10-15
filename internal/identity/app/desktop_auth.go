package app

import (
	"errors"
	"fmt"
	auditapp "github.com/sh2001sh/new-api/internal/audit/app"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"strings"
	"time"
)

const (
	desktopAuthSessionTTL       = 10 * time.Minute
	desktopAuthDeviceTokenTTL   = 7 * 24 * time.Hour
	desktopAuthDefaultPollDelay = 5
)

type DesktopAuthStartRequest struct {
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
	AppVersion string `json:"app_version"`
}

type DesktopAuthStartResponse struct {
	SessionID       string `json:"session_id"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type DesktopAuthPollRequest struct {
	SessionID string `json:"session_id"`
}

type DesktopAuthPollResponse struct {
	Status        string   `json:"status"`
	Authenticated bool     `json:"authenticated"`
	AccessToken   string   `json:"access_token,omitempty"`
	UserID        int      `json:"user_id,omitempty"`
	ServerAddress string   `json:"server_address,omitempty"`
	LastUsername  string   `json:"last_username,omitempty"`
	DeviceID      int      `json:"device_id,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
}

type DesktopAuthorizeRequest struct {
	SessionID string `json:"session_id"`
}

type DesktopAuthorizedDeviceItem struct {
	ID         int      `json:"id"`
	DeviceName string   `json:"device_name"`
	Platform   string   `json:"platform"`
	AppVersion string   `json:"app_version"`
	Scopes     []string `json:"scopes"`
	Status     string   `json:"status"`
	CreatedAt  int64    `json:"created_at"`
	LastUsedAt int64    `json:"last_used_at"`
	ExpiresAt  int64    `json:"expires_at"`
	RevokedAt  int64    `json:"revoked_at"`
}

// DesktopAuthPermissions returns the current desktop authorization grant list.
func DesktopAuthPermissions() []string {
	return []string{
		"View Code Go balance and account summary",
		"Read usage logs and trends",
		"Create and manage desktop tokens",
		"Read configuration templates for supported tools",
		"Apply desktop import and local tool configuration changes",
		"Revoke this desktop device later from profile security settings",
	}
}

// DesktopAuthPollInterval returns the polling interval used by desktop auth clients.
func DesktopAuthPollInterval() int {
	return desktopAuthDefaultPollDelay
}

// BuildDesktopVerificationURI builds the browser approval URL for a desktop auth session.
func BuildDesktopVerificationURI(sessionID string, userCode string) string {
	base := NormalizeDesktopServerAddress("")
	return fmt.Sprintf("%s/desktop/authorize?session_id=%s&code=%s", base, sessionID, userCode)
}

// StartDesktopAuthSession creates a new pending desktop authorization session.
func StartDesktopAuthSession(req DesktopAuthStartRequest) (*DesktopAuthStartResponse, error) {
	deviceName := strings.TrimSpace(req.DeviceName)
	if deviceName == "" {
		deviceName = "Code Go Desktop"
	}
	session := &identitydomain.DesktopAuthSession{
		SessionID:  createDesktopSessionID(),
		UserCode:   createDesktopUserCode(),
		DeviceName: deviceName,
		Platform:   strings.TrimSpace(req.Platform),
		AppVersion: strings.TrimSpace(req.AppVersion),
		Status:     identitydomain.DesktopAuthSessionStatusPending,
		CreatedAt:  platformruntime.GetTimestamp(),
		ExpiresAt:  platformruntime.GetTimestamp() + int64(desktopAuthSessionTTL/time.Second),
	}
	if err := platformdb.DB.Create(session).Error; err != nil {
		return nil, err
	}

	return &DesktopAuthStartResponse{
		SessionID:       session.SessionID,
		UserCode:        session.UserCode,
		VerificationURI: BuildDesktopVerificationURI(session.SessionID, session.UserCode),
		ExpiresIn:       int64(desktopAuthSessionTTL / time.Second),
		Interval:        desktopAuthDefaultPollDelay,
	}, nil
}

// BuildDesktopAuthSessionView returns the browser approval page payload for a session.
func BuildDesktopAuthSessionView(sessionID string, userCode string) (map[string]any, error) {
	session, err := getDesktopAuthSessionByID(strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	if session.UserCode != strings.ToUpper(strings.TrimSpace(userCode)) {
		return nil, errors.New("desktop authorization code does not match")
	}
	if session.ExpiresAt <= platformruntime.GetTimestamp() {
		_ = platformdb.DB.Model(session).Update("status", identitydomain.DesktopAuthSessionStatusExpired).Error
		return nil, errors.New("desktop authorization session has expired")
	}

	return map[string]any{
		"session_id":  session.SessionID,
		"user_code":   session.UserCode,
		"device_name": session.DeviceName,
		"platform":    session.Platform,
		"app_version": session.AppVersion,
		"status":      session.Status,
		"created_at":  session.CreatedAt,
		"approved_at": session.ApprovedAt,
		"expires_at":  session.ExpiresAt,
		"permissions": DesktopAuthPermissions(),
	}, nil
}

// ApproveDesktopAuthSession approves a pending desktop session and issues a device token.
func ApproveDesktopAuthSession(userID int, sessionID string) (map[string]any, error) {
	session, err := getDesktopAuthSessionByID(strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	if session.ExpiresAt <= platformruntime.GetTimestamp() {
		_ = platformdb.DB.Model(session).Update("status", identitydomain.DesktopAuthSessionStatusExpired).Error
		return nil, errors.New("desktop authorization session has expired")
	}
	if session.Status != identitydomain.DesktopAuthSessionStatusPending {
		return nil, errors.New("desktop authorization session has already been handled")
	}

	accessToken, err := createDesktopDeviceAccessToken()
	if err != nil {
		return nil, err
	}

	now := platformruntime.GetTimestamp()
	device := &identitydomain.DesktopAuthorizedDevice{
		UserID:      userID,
		DeviceName:  session.DeviceName,
		Platform:    session.Platform,
		AppVersion:  session.AppVersion,
		AccessToken: accessToken,
		Scopes:      SerializeDesktopScopes(DefaultDesktopScopes()),
		Status:      identitydomain.DesktopAuthorizedDeviceStatusActive,
		CreatedAt:   now,
		LastUsedAt:  now,
		ExpiresAt:   now + int64(desktopAuthDeviceTokenTTL/time.Second),
	}

	if err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(device).Error; err != nil {
			return err
		}
		return tx.Model(session).Updates(map[string]any{
			"user_id":     userID,
			"device_id":   device.Id,
			"status":      identitydomain.DesktopAuthSessionStatusApproved,
			"approved_at": now,
		}).Error
	}); err != nil {
		return nil, err
	}

	auditapp.RecordLog(userID, auditschema.LogTypeManage, fmt.Sprintf("approved desktop device %s", session.DeviceName))
	return map[string]any{
		"device_id":    device.Id,
		"device_name":  device.DeviceName,
		"scopes":       ParseDesktopScopes(device.Scopes),
		"status":       identitydomain.DesktopAuthSessionStatusApproved,
		"approved_at":  now,
		"expires_at":   device.ExpiresAt,
		"access_token": accessToken,
	}, nil
}

// RejectDesktopAuthSession rejects a pending desktop session.
func RejectDesktopAuthSession(userID int, sessionID string) (map[string]any, error) {
	session, err := getDesktopAuthSessionByID(strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	if session.ExpiresAt <= platformruntime.GetTimestamp() {
		_ = platformdb.DB.Model(session).Update("status", identitydomain.DesktopAuthSessionStatusExpired).Error
		return nil, errors.New("desktop authorization session has expired")
	}
	if session.Status != identitydomain.DesktopAuthSessionStatusPending {
		return nil, errors.New("desktop authorization session has already been handled")
	}

	now := platformruntime.GetTimestamp()
	if err := platformdb.DB.Model(session).Updates(map[string]any{
		"user_id":     userID,
		"status":      identitydomain.DesktopAuthSessionStatusRejected,
		"approved_at": now,
	}).Error; err != nil {
		return nil, err
	}

	auditapp.RecordLog(userID, auditschema.LogTypeManage, fmt.Sprintf("rejected desktop device %s", session.DeviceName))
	return map[string]any{
		"session_id":  session.SessionID,
		"status":      identitydomain.DesktopAuthSessionStatusRejected,
		"approved_at": now,
	}, nil
}

// PollDesktopAuthSession returns the current desktop authorization poll state.
func PollDesktopAuthSession(req DesktopAuthPollRequest) (*DesktopAuthPollResponse, error) {
	session, err := getDesktopAuthSessionByID(strings.TrimSpace(req.SessionID))
	if err != nil {
		return nil, err
	}
	if session.ExpiresAt <= platformruntime.GetTimestamp() && session.Status == identitydomain.DesktopAuthSessionStatusPending {
		_ = platformdb.DB.Model(session).Update("status", identitydomain.DesktopAuthSessionStatusExpired).Error
		session.Status = identitydomain.DesktopAuthSessionStatusExpired
	}

	response := &DesktopAuthPollResponse{
		Status:        session.Status,
		Authenticated: false,
	}

	if session.Status == identitydomain.DesktopAuthSessionStatusApproved && session.DeviceID > 0 {
		device, err := getDesktopAuthorizedDeviceByID(session.UserID, session.DeviceID)
		if err != nil {
			return nil, err
		}
		user, err := LoadUserByID(session.UserID, false)
		if err != nil {
			return nil, err
		}
		response.Authenticated = true
		response.AccessToken = device.AccessToken
		response.UserID = session.UserID
		response.ServerAddress = NormalizeDesktopServerAddress("")
		response.LastUsername = user.Username
		response.DeviceID = device.Id
		response.Scopes = ParseDesktopScopes(device.Scopes)
	}

	return response, nil
}

// ListDesktopAuthorizedDevices returns the authorized desktop device list for a user.
func ListDesktopAuthorizedDevices(userID int) ([]DesktopAuthorizedDeviceItem, error) {
	devices, err := listDesktopAuthorizedDevicesByUserID(userID)
	if err != nil {
		return nil, err
	}

	items := make([]DesktopAuthorizedDeviceItem, 0, len(devices))
	for _, device := range devices {
		items = append(items, DesktopAuthorizedDeviceItem{
			ID:         device.Id,
			DeviceName: device.DeviceName,
			Platform:   device.Platform,
			AppVersion: device.AppVersion,
			Scopes:     ParseDesktopScopes(device.Scopes),
			Status:     device.Status,
			CreatedAt:  device.CreatedAt,
			LastUsedAt: device.LastUsedAt,
			ExpiresAt:  device.ExpiresAt,
			RevokedAt:  device.RevokedAt,
		})
	}
	return items, nil
}

// RevokeDesktopAuthorizedDevice revokes an authorized desktop device.
func RevokeDesktopAuthorizedDevice(userID int, deviceID int) error {
	if err := revokeDesktopAuthorizedDevice(userID, deviceID); err != nil {
		return err
	}
	auditapp.RecordLog(userID, auditschema.LogTypeManage, fmt.Sprintf("revoked desktop device %d", deviceID))
	return nil
}

func createDesktopUserCode() string {
	return strings.ToUpper(platformruntime.GetRandomString(8))
}

func createDesktopSessionID() string {
	return platformruntime.GetRandomString(40)
}

func createDesktopDeviceAccessToken() (string, error) {
	key, err := platformruntime.GenerateRandomKey(48)
	if err != nil {
		return "", err
	}
	return "desktop_" + key, nil
}
