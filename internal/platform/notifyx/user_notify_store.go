package notifyx

import (
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

func loadRootUserNotificationTarget() (*identityschema.UserBase, error) {
	var user identityschema.User
	if err := platformdb.DB.
		Select("id", "email", "setting").
		Where("role = ?", constant.RoleRootUser).
		First(&user).Error; err != nil {
		return nil, err
	}
	return user.ToBaseUser(), nil
}
