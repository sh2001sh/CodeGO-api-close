package app

import (
	"testing"

	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSavedPoolsPrunePreviouslySuspendedAndDeletedGroups(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &marketplaceschema.AutoRoutePoolMember{},
		&marketplaceschema.RoutePool{}, &marketplaceschema.RoutePoolMember{}))
	active := autoRouteTestGroup("active", "channel-active", 11, 1)
	suspended := autoRouteTestGroup("suspended", "channel-suspended", 12, 1)
	suspended.LifecycleStatus = marketplacedomain.LifecycleSuspended
	require.NoError(t, db.Create([]marketplaceschema.Group{active, suspended}).Error)
	require.NoError(t, db.Create(&marketplaceschema.RoutePool{ID: "saved", OwnerUserID: 20, Name: "saved"}).Error)
	for priority, id := range []string{"suspended", "deleted", "active"} {
		require.NoError(t, db.Create(&marketplaceschema.AutoRoutePoolMember{OwnerUserID: 20, GroupID: id, Priority: priority + 1}).Error)
		require.NoError(t, db.Create(&marketplaceschema.RoutePoolMember{PoolID: "saved", GroupID: id, Priority: priority + 1}).Error)
	}

	selected, err := loadAutoRoutePoolSelection(20)
	require.NoError(t, err)
	require.Equal(t, map[string]int{"active": 3}, selected)
	_, named, err := loadRoutePool(20, "saved")
	require.NoError(t, err)
	require.Equal(t, map[string]int{"active": 3}, named)
	for _, id := range []string{"suspended", "deleted"} {
		require.ErrorIs(t, db.First(&marketplaceschema.AutoRoutePoolMember{}, "group_id = ?", id).Error, gorm.ErrRecordNotFound)
		require.ErrorIs(t, db.First(&marketplaceschema.RoutePoolMember{}, "group_id = ?", id).Error, gorm.ErrRecordNotFound)
	}
}
