package app

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	httpctx "github.com/sh2001sh/new-api/internal/platform/transport/http/httpctx"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBoundRoutePoolRejectsDirectChannelOutsidePool(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&marketplaceschema.Channel{}, &marketplaceschema.Group{},
		&marketplaceschema.RoutePool{}, &marketplaceschema.RoutePoolMember{},
		&marketplaceschema.RankingSnapshot{},
	))
	insideID, outsideID := 101, 202
	insideChannel := marketplaceschema.Channel{
		ID: "inside-channel", OwnerUserID: 10, DeclaredModels: `["gpt-5"]`,
		InternalChannelID: &insideID, Status: marketplacedomain.LifecycleActive,
	}
	outsideChannel := marketplaceschema.Channel{
		ID: "outside-channel", OwnerUserID: 11, DeclaredModels: `["gpt-5"]`,
		InternalChannelID: &outsideID, Status: marketplacedomain.LifecycleActive,
	}
	insideGroup := marketplaceschema.Group{
		ID: "inside-group", ChannelID: insideChannel.ID, OwnerUserID: 10,
		InternalGroupName: "market_inside", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility: marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, db.Create(&insideChannel).Error)
	require.NoError(t, db.Create(&outsideChannel).Error)
	require.NoError(t, db.Create(&insideGroup).Error)
	require.NoError(t, db.Create(&marketplaceschema.RoutePool{ID: "strict-pool", OwnerUserID: 20, Name: "严格池", Strategy: "priority"}).Error)
	require.NoError(t, db.Create(&marketplaceschema.RoutePoolMember{PoolID: "strict-pool", GroupID: insideGroup.ID, Priority: 1}).Error)

	ctx, _ := gin.CreateTestContext(nil)
	httpctx.SetContextKey(ctx, constant.ContextKeyUserId, 20)
	httpctx.SetContextKey(ctx, constant.ContextKeyTokenGroup, RoutePoolTokenGroupValue("strict-pool"))
	require.NoError(t, RequireChannelInBoundRoutePool(ctx, "gpt-5", insideID))
	require.ErrorIs(t, RequireChannelInBoundRoutePool(ctx, "gpt-5", outsideID), ErrChannelOutsideBoundRoutePool)
}

func TestMarketplaceTokenBindingIsStableAndAllowsSelfConsumption(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}, &identityschema.Token{}))

	group := marketplaceschema.Group{
		ID: "group-1", ChannelID: "channel-1", OwnerUserID: 10,
		PublicSlug: "market-group-1", SystemDisplayName: "用户分组 1.00x · #0001",
		InternalGroupName: "market_u0100_group1", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility: marketplacedomain.VisibilityPublic,
	}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: group.ChannelID, OwnerUserID: group.OwnerUserID, ProviderType: "openai_compatible", BaseURLCiphertext: "url", CredentialCiphertext: "key", ModelPrices: `{"gpt-5":{"input_price_per_million":2,"output_price_per_million":8}}`}).Error)
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
	require.Equal(t, float64(2), binding.ModelPrices["gpt-5"].InputPricePerMillion)
}

func TestMarketplacePrivateGroupOnlyAllowsOwner(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}))

	group := marketplaceschema.Group{
		ID: "private-group", ChannelID: "private-channel", OwnerUserID: 10,
		PublicSlug: "private-market-group", SystemDisplayName: "私有分组",
		InternalGroupName: "market_u10_private_1x_group_v1", SourceType: marketplacedomain.SourceTypeMarketplaceUser,
		CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1,
		LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: marketplacedomain.VerificationPassed,
		Visibility: marketplacedomain.VisibilityPrivate,
	}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: group.ChannelID, OwnerUserID: group.OwnerUserID, ProviderType: "openai_compatible", BaseURLCiphertext: "url", CredentialCiphertext: "key"}).Error)

	_, err := ResolveTokenGroupBinding(TokenGroupValue(group.ID), 20)
	require.EqualError(t, err, "市场分组未公开或无权访问")

	binding, err := ResolveTokenGroupBinding(TokenGroupValue(group.ID), group.OwnerUserID)
	require.NoError(t, err)
	require.Equal(t, group.InternalGroupName, binding.InternalGroup)
}

func TestMarketplaceTokenBindingCreatesTokenWhenNoneSelected(t *testing.T) {
	db := openMarketplaceAppTestDB(t)
	require.NoError(t, db.AutoMigrate(&marketplaceschema.Channel{}, &marketplaceschema.Group{}, &identityschema.Token{}))
	group := marketplaceschema.Group{ID: "auto-group", ChannelID: "auto-channel", OwnerUserID: 10, PublicSlug: "auto", SystemDisplayName: "自动分组", InternalGroupName: "market_auto", SourceType: marketplacedomain.SourceTypeMarketplaceUser, CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly, Multiplier: 1, LifecycleStatus: marketplacedomain.LifecycleActive, VerificationStatus: marketplacedomain.VerificationPassed, Visibility: marketplacedomain.VisibilityPublic}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&marketplaceschema.Channel{ID: group.ChannelID, OwnerUserID: group.OwnerUserID, ProviderType: "openai_compatible", DeclaredModels: `["gpt-5"]`, BaseURLCiphertext: "url", CredentialCiphertext: "key"}).Error)
	id, err := BindTokenToMarketplaceGroupResult(20, 0, group.ID)
	require.NoError(t, err)
	require.Positive(t, id)
	var token identityschema.Token
	require.NoError(t, db.First(&token, id).Error)
	require.Equal(t, 20, token.UserId)
	require.Equal(t, "market:auto-group", token.Group)
	require.True(t, token.UnlimitedQuota)
	require.NotEmpty(t, token.Key)
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
