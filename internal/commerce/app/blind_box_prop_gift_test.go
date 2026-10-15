package app

import (
	"testing"

	"github.com/sh2001sh/new-api/constant"
	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/stretchr/testify/require"
)

func TestGiftBlindBoxPropTransfersOwnershipIdempotently(t *testing.T) {
	db := setupRedemptionTestDB(t)
	sender := blindBoxPropGiftUser(9971, "PGF001", "prop-gift-sender")
	recipient := blindBoxPropGiftUser(9972, "PGF002", "prop-gift-recipient")
	require.NoError(t, db.Create(sender).Error)
	require.NoError(t, db.Create(recipient).Error)
	prop := &commerceschema.BlindBoxProp{
		UserId: sender.Id, OpenRecordId: 8811,
		PropType: commerceschema.BlindBoxPropTypeMonthlyPassMultiplier,
		Title:    "0.10 倍率体验卡", Status: commerceschema.BlindBoxPropStatusAvailable,
		DurationSeconds: 900, RemainingSeconds: 900, BenefitReference: "gift-origin",
	}
	require.NoError(t, db.Create(prop).Error)

	request := GiftBlindBoxPropRequest{
		RecipientExternalId: recipient.ExternalId,
		RequestId:           "prop-gift-idempotent",
	}
	created, err := GiftBlindBoxProp(sender.Id, prop.Id, request)
	require.NoError(t, err)
	require.Equal(t, recipient.ExternalId, created.Recipient.ExternalId)
	require.Equal(t, prop.Id, created.Gift.PropId)

	replayed, err := GiftBlindBoxProp(sender.Id, prop.Id, request)
	require.NoError(t, err)
	require.Equal(t, created.Gift.Id, replayed.Gift.Id)

	var saved commerceschema.BlindBoxProp
	require.NoError(t, db.First(&saved, prop.Id).Error)
	require.Equal(t, recipient.Id, saved.UserId)
	require.Equal(t, prop.OpenRecordId, saved.OpenRecordId)
	require.Equal(t, "gift-origin", saved.BenefitReference)

	_, err = ActivateBlindBoxProp(sender.Id, prop.Id)
	require.Error(t, err)
	activated, err := ActivateBlindBoxProp(recipient.Id, prop.Id)
	require.NoError(t, err)
	require.Equal(t, commerceschema.BlindBoxPropStatusActive, activated.Status)

	var giftCount, auditCount int64
	require.NoError(t, db.Model(&commerceschema.BlindBoxPropGift{}).Count(&giftCount).Error)
	require.Equal(t, int64(1), giftCount)
	require.NoError(t, db.Model(&auditschema.Log{}).
		Where("user_id IN ? AND type = ?", []int{sender.Id, recipient.Id}, auditschema.LogTypeManage).
		Count(&auditCount).Error)
	require.Equal(t, int64(2), auditCount)
}

func TestGiftBlindBoxPropRejectsUnsafeTransfers(t *testing.T) {
	db := setupRedemptionTestDB(t)
	sender := blindBoxPropGiftUser(9973, "PGF003", "prop-gift-owner")
	recipient := blindBoxPropGiftUser(9974, "PGF004", "prop-gift-target")
	disabled := blindBoxPropGiftUser(9975, "PGF005", "prop-gift-disabled")
	disabled.Status = constant.UserStatusDisabled
	require.NoError(t, db.Create([]*identityschema.User{sender, recipient, disabled}).Error)

	statuses := []string{
		commerceschema.BlindBoxPropStatusActive,
		commerceschema.BlindBoxPropStatusPaused,
		commerceschema.BlindBoxPropStatusReserved,
		commerceschema.BlindBoxPropStatusUsed,
		commerceschema.BlindBoxPropStatusExpired,
	}
	for index, status := range statuses {
		prop := &commerceschema.BlindBoxProp{
			UserId: sender.Id, PropType: commerceschema.BlindBoxPropTypeConsumeDiscount95,
			Title: status, Status: status,
		}
		require.NoError(t, db.Create(prop).Error)
		_, err := GiftBlindBoxProp(sender.Id, prop.Id, GiftBlindBoxPropRequest{
			RecipientExternalId: recipient.ExternalId,
			RequestId:           "prop-gift-status-" + string(rune('a'+index)),
		})
		require.ErrorContains(t, err, "只有未使用且未锁定")
	}

	available := &commerceschema.BlindBoxProp{
		UserId: sender.Id, PropType: commerceschema.BlindBoxPropTypeConsumeDiscount95,
		Title: "可赠送道具", Status: commerceschema.BlindBoxPropStatusAvailable,
	}
	require.NoError(t, db.Create(available).Error)
	_, err := GiftBlindBoxProp(sender.Id, available.Id, GiftBlindBoxPropRequest{
		RecipientExternalId: sender.ExternalId, RequestId: "prop-gift-self",
	})
	require.ErrorIs(t, err, commerceschema.ErrWalletTransferSelf)
	_, err = GiftBlindBoxProp(sender.Id, available.Id, GiftBlindBoxPropRequest{
		RecipientExternalId: disabled.ExternalId, RequestId: "prop-gift-disabled",
	})
	require.ErrorIs(t, err, commerceschema.ErrWalletTransferRecipientNotFound)
	_, err = GiftBlindBoxProp(sender.Id, available.Id, GiftBlindBoxPropRequest{
		RecipientExternalId: recipient.ExternalId, RequestId: "prop-gift-conflict",
	})
	require.NoError(t, err)
	_, err = GiftBlindBoxProp(sender.Id, available.Id+1, GiftBlindBoxPropRequest{
		RecipientExternalId: recipient.ExternalId, RequestId: "prop-gift-conflict",
	})
	require.ErrorContains(t, err, "请求冲突")
}

func blindBoxPropGiftUser(id int, externalID, username string) *identityschema.User {
	return &identityschema.User{
		Id: id, ExternalId: externalID, Username: username,
		AffCode: externalID, Status: constant.UserStatusEnabled,
	}
}
