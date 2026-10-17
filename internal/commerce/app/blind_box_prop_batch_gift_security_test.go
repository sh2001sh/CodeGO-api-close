package app

import (
	"testing"

	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestBatchBlindBoxGiftReplaysWithoutSpendingMoreInventory(t *testing.T) {
	db := setupRedemptionTestDB(t)
	for _, user := range []identityschema.User{
		{Id: 8191, ExternalId: "BGS001", Username: "batch-gift-sender", AffCode: "BGS001", Status: constant.UserStatusEnabled},
		{Id: 8192, ExternalId: "BGS002", Username: "batch-gift-recipient", AffCode: "BGS002", Status: constant.UserStatusEnabled},
	} {
		require.NoError(t, db.Create(&user).Error)
	}
	for range 3 {
		require.NoError(t, db.Create(&commerceschema.BlindBoxProp{UserId: 8191, Status: commerceschema.BlindBoxPropStatusAvailable, Title: "test gift", PropType: "discount"}).Error)
	}
	for range 2 {
		require.NoError(t, GiftBlindBoxPropsBatch(8191, "BGS002", 2, "same-batch"))
	}
	require.ErrorContains(t, GiftBlindBoxPropsBatch(8191, "BGS002", 3, "same-batch"), "冲突")
	var recipientCount, senderCount, gifts int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxProp{}).Where("user_id = ?", 8192).Count(&recipientCount).Error)
	require.NoError(t, db.Model(&commerceschema.BlindBoxProp{}).Where("user_id = ?", 8191).Count(&senderCount).Error)
	require.NoError(t, db.Model(&commerceschema.BlindBoxPropGift{}).Count(&gifts).Error)
	require.EqualValues(t, 2, recipientCount)
	require.EqualValues(t, 1, senderCount)
	require.EqualValues(t, 2, gifts)
	// The first slot would succeed, but the whole recipient transaction rolls back.
	require.Error(t, GiftBlindBoxPropsBatch(8191, "BGS002", 2, "insufficient-stock"))
	require.NoError(t, db.Model(&commerceschema.BlindBoxProp{}).Where("user_id = ?", 8191).Count(&senderCount).Error)
	require.EqualValues(t, 1, senderCount)
	require.Error(t, GiftBlindBoxPropsBatch(8191, "BGS001", 1, "self-gift"))
	require.Error(t, GiftBlindBoxPropsBatch(8191, "BGS002", 51, "oversized"))
	require.NoError(t, db.Model(&identityschema.User{}).Where("id = ?", 8192).Update("status", constant.UserStatusDisabled).Error)
	require.Error(t, GiftBlindBoxPropsBatch(8191, "BGS002", 1, "disabled"))
}
