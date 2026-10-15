package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
)

const marketplaceInviteTokenBytes = 32

type GroupInviteView struct {
	Token     string     `json:"token,omitempty"`
	GroupID   string     `json:"group_id"`
	GroupName string     `json:"group_name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateMarketplaceGroupInvite revokes previous active links and creates one
// new opaque token. Only its hash is persisted.
func CreateMarketplaceGroupInvite(ownerUserID int, groupID string) (*GroupInviteView, error) {
	var group marketplaceschema.Group
	if err := platformdb.DB.First(&group, "id = ?", strings.TrimSpace(groupID)).Error; err != nil {
		return nil, err
	}
	if group.OwnerUserID != ownerUserID {
		return nil, errors.New("无权为该分组创建邀请链接")
	}
	if !marketplacedomain.AcceptsTraffic(group.LifecycleStatus) {
		return nil, errors.New("分组当前不可用，无法创建邀请链接")
	}
	raw := make([]byte, marketplaceInviteTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(30 * 24 * time.Hour)
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Model(&marketplaceschema.GroupInvite{}).
			Where("group_id = ? AND revoked_at IS NULL", group.ID).
			Update("revoked_at", now).Error; err != nil {
			return err
		}
		return tx.Create(&marketplaceschema.GroupInvite{
			GroupID: group.ID, CreatedBy: ownerUserID,
			TokenHash: base64.RawURLEncoding.EncodeToString(hash[:]), ExpiresAt: &expires,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return &GroupInviteView{Token: token, GroupID: group.ID, GroupName: group.SystemDisplayName, ExpiresAt: &expires}, nil
}

func AcceptMarketplaceGroupInvite(userID int, rawToken string) (*GroupInviteView, error) {
	if userID <= 0 {
		return nil, errors.New("用户未登录")
	}
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, errors.New("邀请链接无效")
	}
	hash := sha256.Sum256([]byte(rawToken))
	encodedHash := base64.RawURLEncoding.EncodeToString(hash[:])
	var invite marketplaceschema.GroupInvite
	if err := platformdb.DB.Where("token_hash = ? AND revoked_at IS NULL", encodedHash).First(&invite).Error; err != nil {
		return nil, errors.New("邀请链接无效或已失效")
	}
	if invite.ExpiresAt != nil && invite.ExpiresAt.Before(time.Now().UTC()) {
		return nil, errors.New("邀请链接已过期")
	}
	var group marketplaceschema.Group
	if err := platformdb.DB.First(&group, "id = ?", invite.GroupID).Error; err != nil {
		return nil, err
	}
	if !marketplacedomain.AcceptsTraffic(group.LifecycleStatus) || group.VerificationStatus != marketplacedomain.VerificationPassed {
		return nil, errors.New("分组当前不可用")
	}
	access := marketplaceschema.GroupAccess{GroupID: group.ID, UserID: userID, GrantedByInvite: invite.ID}
	if err := platformdb.DB.Where("group_id = ? AND user_id = ?", group.ID, userID).
		FirstOrCreate(&access).Error; err != nil {
		return nil, err
	}
	return &GroupInviteView{GroupID: group.ID, GroupName: group.SystemDisplayName, ExpiresAt: invite.ExpiresAt}, nil
}

func HasMarketplaceGroupAccess(groupID string, userID int) bool {
	if userID <= 0 {
		return false
	}
	var group marketplaceschema.Group
	if platformdb.DB.Select("id", "owner_user_id", "visibility").First(&group, "id = ?", groupID).Error != nil {
		return false
	}
	return hasMarketplaceGroupAccessForGroup(&group, userID)
}

func hasMarketplaceGroupAccessForGroup(group *marketplaceschema.Group, userID int) bool {
	if group == nil || userID <= 0 {
		return false
	}
	if group.OwnerUserID == userID || group.Visibility == marketplacedomain.VisibilityPublic {
		return true
	}
	var count int64
	return platformdb.DB.Model(&marketplaceschema.GroupAccess{}).
		Where("group_id = ? AND user_id = ?", group.ID, userID).Count(&count).Error == nil && count > 0
}
