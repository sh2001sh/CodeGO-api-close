package app

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/sh2001sh/new-api/constant"
	communityschema "github.com/sh2001sh/new-api/internal/community/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	marketplacedomain "github.com/sh2001sh/new-api/internal/marketplace/domain"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAuthorizeCommunityServiceUsesDedicatedMinimumLengthSecret(t *testing.T) {
	t.Setenv(CommunityAPISecretEnvironment, "")
	require.ErrorIs(t, AuthorizeCommunityService("anything"), ErrCommunityAPIDisabled)
	t.Setenv(CommunityAPISecretEnvironment, "too-short")
	require.ErrorIs(t, AuthorizeCommunityService("too-short"), ErrCommunityAPIDisabled)

	secret := strings.Repeat("s", 32)
	t.Setenv(CommunityAPISecretEnvironment, secret)
	require.ErrorIs(t, AuthorizeCommunityService(strings.Repeat("x", 32)), ErrCommunityAPIUnauthorized)
	require.NoError(t, AuthorizeCommunityService(secret))
}

func setupCommunityBridgeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := platformdb.DB
	originalPostgreSQL := platformdb.UsingPostgreSQL
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	platformdb.DB = db
	platformdb.UsingPostgreSQL = false
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	require.NoError(t, db.AutoMigrate(&identityschema.User{}, &marketplaceschema.Channel{}, &marketplaceschema.Group{}, &communityschema.ChannelRating{}))
	return db
}

func TestGetCommunityMemberExposesOnlyPublicIdentityAndVerifiedOwnerState(t *testing.T) {
	db := setupCommunityBridgeTestDB(t)
	user := identityschema.User{ExternalId: "ABC234", Username: "member", DisplayName: "成员", Password: "unused-password", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)

	createBridgeChannel(t, db, user.Id, "1001", "public-active", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)
	createBridgeChannel(t, db, user.Id, "1002", "private-active", marketplacedomain.VisibilityPrivate, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)
	createBridgeChannel(t, db, user.Id, "1003", "public-failed", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationFailed, marketplacedomain.LifecycleActive)

	member, err := GetCommunityMember("ABC234")
	require.NoError(t, err)
	require.Equal(t, CommunityMember{
		Subject: "ABC234", Active: true, Username: "member", DisplayName: "成员", VerifiedChannelOwner: true,
	}, *member)
}

func TestGetCommunityMemberReturnsInactiveWithoutProfileFields(t *testing.T) {
	db := setupCommunityBridgeTestDB(t)
	user := identityschema.User{ExternalId: "DEF567", Username: "disabled", DisplayName: "Disabled", Password: "unused-password", Status: constant.UserStatusDisabled}
	require.NoError(t, db.Create(&user).Error)

	member, err := GetCommunityMember("DEF567")
	require.NoError(t, err)
	require.Equal(t, CommunityMember{Subject: "DEF567", Active: false}, *member)
	_, err = ListCommunityMemberChannels("DEF567", 1, 20, "", "", "")
	require.ErrorIs(t, err, ErrCommunityMemberInactive)
}

func TestListCommunityMemberChannelsReturnsOnlyEligiblePublicSummaries(t *testing.T) {
	db := setupCommunityBridgeTestDB(t)
	user := identityschema.User{ExternalId: "GHJ678", Username: "owner", Password: "unused-password", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)

	createBridgeChannel(t, db, user.Id, "2001", "eligible", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleDegraded)
	createBridgeChannel(t, db, user.Id, "2002", "unlisted", marketplacedomain.VisibilityUnlisted, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)
	createBridgeChannel(t, db, user.Id, "2003", "suspended", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleSuspended)

	result, err := ListCommunityMemberChannels("GHJ678", 1, 20, "", "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Total)
	require.Equal(t, 1, result.Page)
	require.Equal(t, 20, result.PageSize)
	require.Equal(t, []CommunityChannel{{
		ID: "2001", Slug: "eligible", Name: "eligible name", Provider: "openai_compatible",
		LifecycleStatus: marketplacedomain.LifecycleDegraded, VerificationStatus: marketplacedomain.VerificationPassed,
	}}, result.Items)
}

func TestCommunityBridgeRejectsInvalidOrUnknownSubjects(t *testing.T) {
	setupCommunityBridgeTestDB(t)
	_, err := GetCommunityMember("invalid")
	require.ErrorIs(t, err, ErrInvalidCommunitySubject)
	_, err = GetCommunityMember("ABC234")
	require.ErrorIs(t, err, ErrCommunityMemberNotFound)
}

func TestListCommunitySellersGroupsPublicChannelsAndExcludesInactiveOwners(t *testing.T) {
	db := setupCommunityBridgeTestDB(t)
	first := identityschema.User{ExternalId: "ABC234", Username: "alpha", DisplayName: "Alpha", AffCode: "seller-alpha", Status: constant.UserStatusEnabled}
	second := identityschema.User{ExternalId: "DEF567", Username: "beta", AffCode: "seller-beta", Status: constant.UserStatusEnabled}
	inactive := identityschema.User{ExternalId: "GHJ678", Username: "inactive", AffCode: "seller-inactive", Status: constant.UserStatusDisabled}
	for _, user := range []*identityschema.User{&first, &second, &inactive} {
		require.NoError(t, db.Create(user).Error)
	}
	createBridgeChannel(t, db, first.Id, "101", "first", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)
	createBridgeChannel(t, db, first.Id, "102", "second", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleDegraded)
	createBridgeChannel(t, db, first.Id, "103", "private", marketplacedomain.VisibilityPrivate, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)
	createBridgeChannel(t, db, second.Id, "201", "other", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)
	require.NoError(t, db.Model(&marketplaceschema.Channel{}).Where("id = ?", "201").Update("provider_type", "anthropic").Error)
	createBridgeChannel(t, db, inactive.Id, "301", "inactive", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)

	result, err := ListCommunitySellers(1, 20, "", "", "rating")
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Total)
	require.Len(t, result.Items, 2)
	bySubject := map[string]CommunitySeller{}
	for _, seller := range result.Items {
		bySubject[seller.Subject] = seller
	}
	require.Equal(t, int64(2), bySubject["ABC234"].ChannelCount)
	require.Len(t, bySubject["ABC234"].Channels, 2)
	require.Equal(t, "alpha", bySubject["ABC234"].Username)
	require.Equal(t, int64(1), bySubject["DEF567"].ChannelCount)
	require.Equal(t, "beta", bySubject["DEF567"].DisplayName)
	require.NotContains(t, bySubject, "GHJ678")

	filtered, err := ListCommunitySellers(1, 20, "Alpha", "openai_compatible", "rating")
	require.NoError(t, err)
	require.Equal(t, int64(1), filtered.Total)
	require.Equal(t, "ABC234", filtered.Items[0].Subject)
	anthropic, err := ListCommunitySellers(1, 20, "", "anthropic", "rating")
	require.NoError(t, err)
	require.Equal(t, int64(1), anthropic.Total)
	require.Equal(t, "DEF567", anthropic.Items[0].Subject)
	page, err := ListCommunitySellers(2, 1, "", "", "recent")
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Len(t, page.Items, 1)
	require.NotEqual(t, result.Items[0].Subject, page.Items[0].Subject)
}

func TestListCommunitySellersRejectsInvalidParameters(t *testing.T) {
	for _, args := range []struct {
		page, size        int
		keyword, provider string
	}{
		{0, 20, "", ""}, {1, 51, "", ""}, {1, 20, strings.Repeat("x", 65), ""},
		{1, 20, "", "provider/invalid"},
	} {
		_, err := ListCommunitySellers(args.page, args.size, args.keyword, args.provider, "rating")
		require.ErrorIs(t, err, ErrInvalidCommunityQuery)
	}
	_, err := ListCommunitySellers(1, 20, "", "", "unsupported")
	require.ErrorIs(t, err, ErrInvalidCommunityQuery)
}

func TestCommunitySellerQueryBuildsIndependentStatements(t *testing.T) {
	setupCommunityBridgeTestDB(t)
	countQuery := communitySellerQuery("", "").Distinct("community_users.id")
	pageQuery := communitySellerQuery("", "")

	require.NotSame(t, countQuery.Statement, pageQuery.Statement)
	require.True(t, countQuery.Statement.Distinct)
	require.False(t, pageQuery.Statement.Distinct)
}

func TestCommunityChannelRatingsUpdateAndWeightSellerScore(t *testing.T) {
	db := setupCommunityBridgeTestDB(t)
	owner := identityschema.User{ExternalId: "ABC234", Username: "owner", AffCode: "rating-owner", Status: constant.UserStatusEnabled}
	firstViewer := identityschema.User{ExternalId: "DEF567", Username: "first-viewer", AffCode: "rating-viewer-1", Status: constant.UserStatusEnabled}
	secondViewer := identityschema.User{ExternalId: "GHJ678", Username: "second-viewer", AffCode: "rating-viewer-2", Status: constant.UserStatusEnabled}
	for _, user := range []*identityschema.User{&owner, &firstViewer, &secondViewer} {
		require.NoError(t, db.Create(user).Error)
	}
	createBridgeChannel(t, db, owner.Id, "501", "alpha-channel", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)
	createBridgeChannel(t, db, owner.Id, "502", "beta-channel", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)

	_, err := RateCommunityChannel(CommunityRatingRequest{ViewerSubject: owner.ExternalId, Stars: 5}, "501")
	require.ErrorIs(t, err, ErrCommunitySelfRating)
	_, err = RateCommunityChannel(CommunityRatingRequest{ViewerSubject: firstViewer.ExternalId, Stars: 0}, "501")
	require.ErrorIs(t, err, ErrInvalidCommunityRating)

	_, err = RateCommunityChannel(CommunityRatingRequest{ViewerSubject: firstViewer.ExternalId, Stars: 5}, "501")
	require.NoError(t, err)
	_, err = RateCommunityChannel(CommunityRatingRequest{ViewerSubject: secondViewer.ExternalId, Stars: 3}, "501")
	require.NoError(t, err)
	result, err := RateCommunityChannel(CommunityRatingRequest{ViewerSubject: firstViewer.ExternalId, Stars: 4}, "502")
	require.NoError(t, err)
	require.Equal(t, 4, result.Channel.ViewerStars)
	require.Equal(t, int64(1), result.Channel.RatingCount)
	require.InDelta(t, 8, result.Channel.AverageScore, 0.001)
	require.Equal(t, int64(3), result.Seller.RatingCount)
	require.InDelta(t, 8, result.Seller.AverageScore, 0.001)

	updated, err := RateCommunityChannel(CommunityRatingRequest{ViewerSubject: firstViewer.ExternalId, Stars: 1}, "501")
	require.NoError(t, err)
	require.Equal(t, int64(2), updated.Channel.RatingCount)
	require.InDelta(t, 4, updated.Channel.AverageScore, 0.001)
	require.InDelta(t, 16.0/3.0, updated.Seller.AverageScore, 0.001)
	var ratingRows int64
	require.NoError(t, db.Model(&communityschema.ChannelRating{}).Count(&ratingRows).Error)
	require.Equal(t, int64(3), ratingRows)
}

func TestCommunityChannelSearchSortAndViewerRating(t *testing.T) {
	db := setupCommunityBridgeTestDB(t)
	owner := identityschema.User{ExternalId: "ABC234", Username: "owner", AffCode: "search-owner", Status: constant.UserStatusEnabled}
	viewer := identityschema.User{ExternalId: "DEF567", Username: "viewer", AffCode: "search-viewer", Status: constant.UserStatusEnabled}
	require.NoError(t, db.Create(&owner).Error)
	require.NoError(t, db.Create(&viewer).Error)
	createBridgeChannel(t, db, owner.Id, "601", "quiet-route", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)
	createBridgeChannel(t, db, owner.Id, "602", "rated-route", marketplacedomain.VisibilityPublic, marketplacedomain.VerificationPassed, marketplacedomain.LifecycleActive)
	require.NoError(t, db.Model(&marketplaceschema.Group{}).Where("channel_id = ?", "602").Update("system_display_name", "高分渠道").Error)
	_, err := RateCommunityChannel(CommunityRatingRequest{ViewerSubject: viewer.ExternalId, Stars: 5}, "602")
	require.NoError(t, err)

	channels, err := ListCommunityMemberChannels(owner.ExternalId, 1, 20, "", "rating", viewer.ExternalId)
	require.NoError(t, err)
	require.Len(t, channels.Items, 2)
	require.Equal(t, "602", channels.Items[0].ID)
	require.Equal(t, 5, channels.Items[0].ViewerStars)
	require.InDelta(t, 10, channels.Items[0].AverageScore, 0.001)

	searched, err := ListCommunityMemberChannels(owner.ExternalId, 1, 20, "高分", "name", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), searched.Total)
	require.Equal(t, "602", searched.Items[0].ID)
	directory, err := ListCommunitySellers(1, 20, "高分渠道", "", "rating")
	require.NoError(t, err)
	require.Equal(t, int64(1), directory.Total)
	require.Equal(t, owner.ExternalId, directory.Items[0].Subject)
	require.InDelta(t, 10, directory.Items[0].AverageScore, 0.001)
}

func TestPublicCommunityIdentityDoesNotExposeEmailOrPhoneLoginNames(t *testing.T) {
	username, displayName := publicCommunityIdentity("ABC234", "13089988615@163.com", "13089988615@163.com")
	require.Equal(t, "codego-abc234", username)
	require.Equal(t, "渠道主 ABC234", displayName)

	username, displayName = publicCommunityIdentity("DEF567", "+86 138-0013-8000", "阿青")
	require.Equal(t, "codego-def567", username)
	require.Equal(t, "阿青", displayName)

	username, displayName = publicCommunityIdentity("GHJ678", "learnevery", "")
	require.Equal(t, "learnevery", username)
	require.Equal(t, "learnevery", displayName)
}

func createBridgeChannel(t *testing.T, db *gorm.DB, ownerID int, id, slug, visibility, verification, lifecycle string) {
	t.Helper()
	channel := marketplaceschema.Channel{ID: id, OwnerUserID: ownerID, ProviderType: "openai_compatible", Status: lifecycle}
	group := marketplaceschema.Group{
		ID: "group-" + id, ChannelID: id, OwnerUserID: ownerID, PublicSlug: slug,
		SystemDisplayName: slug + " name", InternalGroupName: "internal-" + id,
		SourceType: marketplacedomain.SourceTypeMarketplaceUser, CreditPoolPolicy: marketplacedomain.CreditPolicyUniversalOnly,
		Multiplier: 1, LifecycleStatus: lifecycle, VerificationStatus: verification, Visibility: visibility,
	}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&group).Error)
}
