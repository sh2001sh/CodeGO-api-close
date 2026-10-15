package app

import (
	"errors"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"strings"
)

func getDesktopAuthSessionByID(sessionID string) (*identitydomain.DesktopAuthSession, error) {
	var session identitydomain.DesktopAuthSession
	err := platformdb.DB.Where("session_id = ?", strings.TrimSpace(sessionID)).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

func getDesktopAuthorizedDeviceByID(userID int, deviceID int) (*identitydomain.DesktopAuthorizedDevice, error) {
	var device identitydomain.DesktopAuthorizedDevice
	err := platformdb.DB.Where("id = ? AND user_id = ?", deviceID, userID).First(&device).Error
	if err != nil {
		return nil, err
	}
	return &device, nil
}

func listDesktopAuthorizedDevicesByUserID(userID int) ([]*identitydomain.DesktopAuthorizedDevice, error) {
	var devices []*identitydomain.DesktopAuthorizedDevice
	err := platformdb.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&devices).Error
	return devices, err
}

func revokeDesktopAuthorizedDevice(userID int, deviceID int) error {
	now := platformruntime.GetTimestamp()
	result := platformdb.DB.Model(&identitydomain.DesktopAuthorizedDevice{}).
		Where("id = ? AND user_id = ?", deviceID, userID).
		Updates(map[string]any{
			"status":     identitydomain.DesktopAuthorizedDeviceStatusRevoked,
			"revoked_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ValidateDesktopDeviceAccessToken loads an active desktop device by bearer token.
func ValidateDesktopDeviceAccessToken(accessToken string) (*identitydomain.DesktopAuthorizedDevice, error) {
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var device identitydomain.DesktopAuthorizedDevice
	err := platformdb.DB.Where("access_token = ?", token).First(&device).Error
	if err != nil {
		return nil, err
	}
	if device.Status != identitydomain.DesktopAuthorizedDeviceStatusActive {
		return nil, errors.New("desktop device is not active")
	}
	now := platformruntime.GetTimestamp()
	if device.RevokedAt > 0 || (device.ExpiresAt > 0 && device.ExpiresAt <= now) {
		return nil, errors.New("desktop device is expired or revoked")
	}
	return &device, nil
}

// TouchDesktopDevice marks the desktop device as recently used.
func TouchDesktopDevice(deviceID int) error {
	return platformdb.DB.Model(&identitydomain.DesktopAuthorizedDevice{}).
		Where("id = ?", deviceID).
		Update("last_used_at", platformruntime.GetTimestamp()).
		Error
}
