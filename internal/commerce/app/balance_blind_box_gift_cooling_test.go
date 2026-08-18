package app

import (
	"errors"
	"testing"
	"time"

	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestBalanceBlindBoxGiftRejectsInventoryPurchasedByNewAccount(t *testing.T) {
	db := setupRedemptionTestDB(t)
	setBalanceBlindBoxTestSetting(t, 10)
	sender := createBalanceBlindBoxTestUser(t, db, 9121, "COOL01", 100)
	recipient := createBalanceBlindBoxTestUser(t, db, 9122, "COOL02", 0)
	sender.CreatedAt = platformruntime.GetTimestamp() - int64(time.Hour.Seconds())
	require.NoError(t, db.Model(sender).Update("created_at", sender.CreatedAt).Error)
	_, err := PurchaseBalanceBlindBoxes(sender.Id, "cooling-purchase", 1)
	require.NoError(t, err)

	_, err = GiftBalanceBlindBoxes(sender.Id, GiftBalanceBlindBoxRequest{
		RecipientExternalId: recipient.ExternalId,
		RequestId:           "cooling-gift",
		Count:               1,
	})
	require.ErrorIs(t, err, ErrBalanceBlindBoxGiftCooling)

	sender.CreatedAt = platformruntime.GetTimestamp() - int64((73 * time.Hour).Seconds())
	require.NoError(t, db.Model(sender).Update("created_at", sender.CreatedAt).Error)
	_, err = GiftBalanceBlindBoxes(sender.Id, GiftBalanceBlindBoxRequest{
		RecipientExternalId: recipient.ExternalId,
		RequestId:           "cooling-gift-after-release",
		Count:               1,
	})
	require.NoError(t, err)
	require.False(t, errors.Is(err, ErrBalanceBlindBoxGiftCooling))
}
