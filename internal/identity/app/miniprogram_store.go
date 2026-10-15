package app

import (
	"errors"
	"github.com/sh2001sh/new-api/constant"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
	"strings"
	"time"
)

type MiniProgramAuthContext struct {
	BoundUserID   int
	BindingStatus string
	Username      string
	Group         string
}

func createMiniProgramBindCode(userID int, createdIP string, ttl time.Duration) (string, *identitydomain.MiniProgramBindCode, error) {
	if userID <= 0 {
		return "", nil, errors.New("invalid user id")
	}
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}

	now := platformruntime.GetTimestamp()
	if err := platformdb.DB.Model(&identitydomain.MiniProgramBindCode{}).
		Where("user_id = ? AND used_at = 0 AND expires_at > ?", userID, now).
		Update("expires_at", now-1).Error; err != nil {
		return "", nil, err
	}

	for attempt := 0; attempt < 5; attempt++ {
		code := normalizeMiniProgramBindCode(platformruntime.GetRandomString(6))
		codeHash := hashMiniProgramBindCode(code)

		var existing int64
		if err := platformdb.DB.Model(&identitydomain.MiniProgramBindCode{}).
			Where("code_hash = ? AND used_at = 0 AND expires_at > ?", codeHash, now).
			Count(&existing).Error; err != nil {
			return "", nil, err
		}
		if existing > 0 {
			continue
		}

		record := &identitydomain.MiniProgramBindCode{
			UserId:    userID,
			CodeHash:  codeHash,
			ExpiresAt: now + int64(ttl.Seconds()),
			CreatedIP: strings.TrimSpace(createdIP),
		}
		if err := platformdb.DB.Create(record).Error; err != nil {
			return "", nil, err
		}
		return code, record, nil
	}

	return "", nil, errors.New("failed to generate bind code")
}

func getActiveMiniProgramBindingByOpenID(openID string) (*identitydomain.UserWeChatBinding, error) {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return nil, nil
	}
	var binding identitydomain.UserWeChatBinding
	err := platformdb.DB.Where("openid = ? AND status = ?", openID, identitydomain.MiniProgramBindingStatusActive).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

func getActiveMiniProgramBindingByUserID(userID int) (*identitydomain.UserWeChatBinding, error) {
	if userID <= 0 {
		return nil, nil
	}
	var binding identitydomain.UserWeChatBinding
	err := platformdb.DB.Where("user_id = ? AND status = ?", userID, identitydomain.MiniProgramBindingStatusActive).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// TouchMiniProgramBindingByOpenID updates last_seen_at for the active mini program binding.
func TouchMiniProgramBindingByOpenID(openID string) error {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return nil
	}
	return platformdb.DB.Model(&identitydomain.UserWeChatBinding{}).
		Where("openid = ? AND status = ?", openID, identitydomain.MiniProgramBindingStatusActive).
		Update("last_seen_at", platformruntime.GetTimestamp()).Error
}

func revokeMiniProgramBindingByUserID(userID int) error {
	if userID <= 0 {
		return nil
	}
	now := platformruntime.GetTimestamp()
	return platformdb.DB.Model(&identitydomain.UserWeChatBinding{}).
		Where("user_id = ? AND status = ?", userID, identitydomain.MiniProgramBindingStatusActive).
		Updates(map[string]any{
			"status":     identitydomain.MiniProgramBindingStatusRevoked,
			"revoked_at": now,
		}).Error
}

func revokeMiniProgramBindingByOpenID(openID string) error {
	openID = strings.TrimSpace(openID)
	if openID == "" {
		return nil
	}
	now := platformruntime.GetTimestamp()
	return platformdb.DB.Model(&identitydomain.UserWeChatBinding{}).
		Where("openid = ? AND status = ?", openID, identitydomain.MiniProgramBindingStatusActive).
		Updates(map[string]any{
			"status":     identitydomain.MiniProgramBindingStatusRevoked,
			"revoked_at": now,
		}).Error
}

func consumeMiniProgramBindCodeAndBind(code string, openID string, unionID string) (*identitydomain.MiniProgramBindCode, *identitydomain.UserWeChatBinding, error) {
	code = normalizeMiniProgramBindCode(code)
	openID = strings.TrimSpace(openID)
	unionID = strings.TrimSpace(unionID)
	if code == "" || openID == "" {
		return nil, nil, errors.New("bind code and openid are required")
	}

	codeHash := hashMiniProgramBindCode(code)
	now := platformruntime.GetTimestamp()

	var bindCode identitydomain.MiniProgramBindCode
	var binding identitydomain.UserWeChatBinding
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("code_hash = ?", codeHash).Order("id desc").First(&bindCode).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("bind code is invalid")
			}
			return err
		}

		bindCode.AttemptCount++
		if bindCode.UsedAt > 0 {
			_ = tx.Model(&bindCode).Update("attempt_count", bindCode.AttemptCount).Error
			return errors.New("bind code has already been used")
		}
		if bindCode.ExpiresAt <= now {
			_ = tx.Model(&bindCode).Updates(map[string]any{
				"attempt_count": bindCode.AttemptCount,
				"expires_at":    now - 1,
			}).Error
			return errors.New("bind code has expired")
		}

		var bindingByOpenID identitydomain.UserWeChatBinding
		err := tx.Where("openid = ? AND status = ?", openID, identitydomain.MiniProgramBindingStatusActive).First(&bindingByOpenID).Error
		if err == nil && bindingByOpenID.UserId != bindCode.UserId {
			_ = tx.Model(&bindCode).Update("attempt_count", bindCode.AttemptCount).Error
			return errors.New("this WeChat account is already bound to another website account")
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var bindingByUser identitydomain.UserWeChatBinding
		err = tx.Where("user_id = ? AND status = ?", bindCode.UserId, identitydomain.MiniProgramBindingStatusActive).First(&bindingByUser).Error
		if err == nil && bindingByUser.OpenID != openID {
			_ = tx.Model(&bindCode).Update("attempt_count", bindCode.AttemptCount).Error
			return errors.New("this website account is already bound to another WeChat account")
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if err := tx.Model(&bindCode).Updates(map[string]any{
			"used_at":       now,
			"attempt_count": bindCode.AttemptCount,
		}).Error; err != nil {
			return err
		}

		switch {
		case bindingByOpenID.Id > 0:
			binding = bindingByOpenID
		case bindingByUser.Id > 0:
			binding = bindingByUser
		default:
			binding = identitydomain.UserWeChatBinding{}
		}

		binding.UserId = bindCode.UserId
		binding.OpenID = openID
		binding.UnionID = unionID
		binding.Status = identitydomain.MiniProgramBindingStatusActive
		binding.BoundAt = now
		binding.RevokedAt = 0
		binding.LastSeenAt = now

		if binding.Id > 0 {
			return tx.Save(&binding).Error
		}
		return tx.Create(&binding).Error
	})
	if err != nil {
		return nil, nil, err
	}

	return &bindCode, &binding, nil
}

func getMiniProgramBindCodeTTLSeconds(record *identitydomain.MiniProgramBindCode) int64 {
	if record == nil {
		return 0
	}
	remaining := record.ExpiresAt - platformruntime.GetTimestamp()
	if remaining < 0 {
		return 0
	}
	return remaining
}

func getMiniProgramUserByID(userID int) (*identityschema.User, error) {
	if userID <= 0 {
		return nil, nil
	}
	return LoadUserByID(userID, false)
}

// LoadMiniProgramAuthContext resolves the active binding and enabled website user for a mini program session.
func LoadMiniProgramAuthContext(openID string) (*MiniProgramAuthContext, error) {
	binding, err := getActiveMiniProgramBindingByOpenID(openID)
	if err != nil || binding == nil {
		return nil, err
	}

	user, err := getMiniProgramUserByID(binding.UserId)
	if err != nil || user == nil || user.Status != constant.UserStatusEnabled {
		return nil, err
	}

	return &MiniProgramAuthContext{
		BoundUserID:   user.Id,
		BindingStatus: binding.Status,
		Username:      user.Username,
		Group:         user.Group,
	}, nil
}

func normalizeMiniProgramBindCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func hashMiniProgramBindCode(code string) string {
	return platformsecurity.HmacSha256(normalizeMiniProgramBindCode(code), platformconfig.SessionSecret)
}
