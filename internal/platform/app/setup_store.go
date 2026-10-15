package app

import (
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
)

func rootUserExists() bool {
	var user identityschema.User
	err := platformdb.DB.Where("role = ?", constant.RoleRootUser).First(&user).Error
	return err == nil
}

func pingDB() error {
	sqlDB, err := platformdb.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}
