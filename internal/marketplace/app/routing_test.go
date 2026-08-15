package app

import (
	"testing"

	"github.com/glebarez/sqlite"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMarketplaceTokenBindingIsStableAndAllowsSelfConsumption(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}, &identityschema.Token{}))

	group := marketplaceschema.Group{
		ID: "group-1", ChannelID: "channel-1", OwnerUserID: 10,
		PublicSlug: "market-group-1", SystemDisplayName: "用户分组 1.00x · #0001",
		InternalGroupName: "market_u0100_group1", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility: marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, db.Create(&group).Error)
	token := identityschema.Token{Id: 1, UserId: 20, Key: "market-token", CrossGroupRetry: true}
	require.NoError(t, db.Create(&token).Error)

	ownerBinding, err := ResolveTokenGroupBinding(TokenGroupValue(group.ID), group.OwnerUserID)
	require.NoError(t, err)
	require.Equal(t, group.OwnerUserID, ownerBinding.OwnerUserID)

	require.NoError(t, BindTokenToMarketplaceGroup(20, token.Id, group.ID))
	var saved identityschema.Token
	require.NoError(t, db.First(&saved, token.Id).Error)
	require.Equal(t, "market:group-1", saved.Group)
	require.False(t, saved.CrossGroupRetry)

	binding, err := ResolveTokenGroupBinding(saved.Group, saved.UserId)
	require.NoError(t, err)
	require.Equal(t, group.InternalGroupName, binding.InternalGroup)
	require.Equal(t, marketplacedomain.CreditPolicyUniversalOnly, binding.CreditPoolPolicy)
}

func TestMarketplacePrivateGroupOnlyAllowsOwner(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Group{}))

	group := marketplaceschema.Group{
		ID: "private-group", ChannelID: "private-channel", OwnerUserID: 10,
		PublicSlug: "private-market-group", SystemDisplayName: "私有分组",
		InternalGroupName: "market_u10_private_1x_group_v1", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility: marketplacedomain.VisibilityPrivate,
	}
	require.NoError(t, db.Create(&group).Error)

	_, err := ResolveTokenGroupBinding(TokenGroupValue(group.ID), 20)
	require.EqualError(t, err, "市场分组未公开或无权访问")

	binding, err := ResolveTokenGroupBinding(TokenGroupValue(group.ID), group.OwnerUserID)
	require.NoError(t, err)
	require.Equal(t, group.InternalGroupName, binding.InternalGroup)
}

func openMarketplaceAppTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := platformdb.DB
	originalSQLite := platformdb.UsingSQLite
	originalPostgreSQL := platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingSQLite = originalSQLite
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.UsingSQLite = true
	platformdb.UsingPostgreSQL = false
	return db
}
