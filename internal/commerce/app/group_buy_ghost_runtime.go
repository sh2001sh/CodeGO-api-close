package app

import (
	"fmt"
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformconfig "github.com/sh2001sh/new-api/internal/platform/config"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformobservability "github.com/sh2001sh/new-api/internal/platform/observability"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	platformsecurity "github.com/sh2001sh/new-api/internal/platform/security"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/rand"
)

var (
	ghostUsernames    = []string{"ghost_user_1", "ghost_user_2", "ghost_user_3"}
	ghostDisplayNames = []string{"用户****", "团友****", "会员****"}
)

// InitGhostUsers ensures ghost users exist before group-buy discovery tasks run.
func InitGhostUsers() error {
	if !platformconfig.IsMasterNode {
		return nil
	}
	_, err := initGhostUsersDB()
	return err
}

func initGhostUsersDB() ([]int, error) {
	ids := make([]int, 0, len(ghostUsernames))
	for index, username := range ghostUsernames {
		var user identityschema.User
		err := platformdb.DB.Where("username = ?", username).First(&user).Error
		if err == nil {
			ids = append(ids, user.Id)
			continue
		}
		if err != gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("failed to check ghost user %s: %w", username, err)
		}

		hashedPassword, err := platformsecurity.Password2Hash("ghost_user_password_" + username)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password for %s: %w", username, err)
		}

		newUser := identityschema.User{
			Username:    username,
			Password:    hashedPassword,
			DisplayName: ghostDisplayNames[index%len(ghostDisplayNames)],
			AffCode:     fmt.Sprintf("ghost%d", index+1),
			Role:        constant.RoleCommonUser,
			Status:      constant.UserStatusEnabled,
			Group:       "ghost",
		}
		if err := platformdb.DB.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "username"}},
			DoNothing: true,
		}).Create(&newUser).Error; err != nil {
			return nil, fmt.Errorf("failed to create ghost user %s: %w", username, err)
		}
		if newUser.Id == 0 {
			if err := platformdb.DB.Where("username = ?", username).First(&newUser).Error; err != nil {
				return nil, fmt.Errorf("failed to load concurrently created ghost user %s: %w", username, err)
			}
		}
		ids = append(ids, newUser.Id)
		platformobservability.SysLog(fmt.Sprintf("created ghost user: id=%d username=%s", newUser.Id, username))
	}

	if len(ids) > 0 {
		platformobservability.SysLog(fmt.Sprintf("ghost users initialized: count=%d ids=%v", len(ids), ids))
	}
	return ids, nil
}

// AddGhostMemberToNewOrder adds one ghost participant to a newly created group-buy order.
func AddGhostMemberToNewOrder(tx *gorm.DB, orderID int64) error {
	if tx == nil {
		return fmt.Errorf("tx is nil")
	}
	ghostUserIDs, err := listGhostUserIDs()
	if err != nil {
		return err
	}
	if len(ghostUserIDs) == 0 {
		return nil
	}

	ghostUserID := ghostUserIDs[rand.Intn(len(ghostUserIDs))]
	member := commerceschema.GroupBuyMember{
		GroupBuyId:   orderID,
		UserId:       ghostUserID,
		OrderId:      0,
		BonusGranted: true,
	}
	if err := tx.Create(&member).Error; err != nil {
		return err
	}
	return tx.Model(&commerceschema.GroupBuyOrder{}).
		Where("id = ?", orderID).
		Updates(map[string]any{
			"current_count": gorm.Expr("current_count + ?", 1),
			"updated_at":    platformruntime.GetTimestamp(),
		}).Error
}
