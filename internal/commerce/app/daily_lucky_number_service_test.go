package app

import (
	"testing"
	"time"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"github.com/stretchr/testify/require"
)

func TestListTodayBlindBoxLuckyNumbersFiltersOwnerDateAndExpiry(t *testing.T) {
	prepareDailyLuckyNumberTestDB(t)
	db := platformDBForDailyLuckyTest(t)
	now := time.Now().Unix()
	drawDate := time.Now().Format(luckyDrawDateLayout)
	numbers := []commerceschema.BlindBoxDailyLuckyNumber{
		{BlindBoxOpenRecordId: 9961, UserId: 9960, DrawDate: drawDate, LuckySuffix: "1234", ExpiresAt: now + 3600},
		{BlindBoxOpenRecordId: 9962, UserId: 9960, DrawDate: drawDate, LuckySuffix: "5678", ExpiresAt: now - 1},
		{BlindBoxOpenRecordId: 9963, UserId: 9960, DrawDate: "2099-12-31", LuckySuffix: "9012", ExpiresAt: now + 3600},
		{BlindBoxOpenRecordId: 9964, UserId: 9969, DrawDate: drawDate, LuckySuffix: "3456", ExpiresAt: now + 3600},
	}
	require.NoError(t, db.Create(&numbers).Error)

	result, err := listTodayBlindBoxLuckyNumbers(9960, drawDate, now)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, 9961, result[0].BlindBoxOpenRecordId)
	require.Equal(t, "1234", result[0].LuckySuffix)
}

func TestListDailyLuckyNumberPublicWinsIncludesEverySettledMatch(t *testing.T) {
	prepareDailyLuckyNumberTestDB(t)
	db := platformDBForDailyLuckyTest(t)
	draw := &commerceschema.SubscriptionLuckyDraw{
		Id:            9941,
		DrawDate:      "2099-01-03",
		WinningNumber: "2402",
		Status:        commerceschema.SubscriptionLuckyDrawStatusCompleted,
	}
	rewards := []commerceschema.SubscriptionLuckyReward{
		{
			Id: 9942, DrawId: draw.Id, UserSubscriptionId: 9952, LuckyNumber: "1002", MatchedDigits: 1,
			FinalRewardQuota: int64(platformruntime.QuotaPerUnit), CreditStatus: commerceschema.SubscriptionLuckyRewardCreditCredited,
		},
		{
			Id: 9943, DrawId: draw.Id, UserSubscriptionId: 9953, LuckyNumber: "3402", MatchedDigits: 2,
			FinalRewardQuota: int64(platformruntime.QuotaPerUnit * 2), CreditStatus: commerceschema.SubscriptionLuckyRewardCreditCredited,
		},
		{
			Id: 9944, DrawId: draw.Id, UserSubscriptionId: 9954, LuckyNumber: "9999", MatchedDigits: 0,
			FinalRewardQuota: 0, CreditStatus: commerceschema.SubscriptionLuckyRewardCreditCredited,
		},
		{
			Id: 9945, DrawId: draw.Id, UserSubscriptionId: 9955, LuckyNumber: "2402", MatchedDigits: 4,
			FinalRewardQuota: int64(platformruntime.QuotaPerUnit * 10), CreditStatus: commerceschema.SubscriptionLuckyRewardCreditPending,
		},
	}
	require.NoError(t, db.Create(draw).Error)
	require.NoError(t, db.Create(&rewards).Error)

	page, err := ListDailyLuckyNumberPublicWins(1, 20, "")
	require.NoError(t, err)
	require.Equal(t, int64(2), page.Total)
	require.Len(t, page.Records, 2)
	require.Equal(t, 2, page.Records[0].MatchedDigits)
	require.Equal(t, 1, page.Records[1].MatchedDigits)
	require.Equal(t, "**02", page.Records[0].LuckySuffix)
}

func TestListDailyLuckyNumberPublicWinsDoesNotDependOnSubscriptionNumberTable(t *testing.T) {
	prepareDailyLuckyNumberTestDB(t)
	db := platformDBForDailyLuckyTest(t)
	draw := &commerceschema.SubscriptionLuckyDraw{Id: 9961, DrawDate: "2099-01-04", WinningNumber: "2402", Status: commerceschema.SubscriptionLuckyDrawStatusCompleted}
	reward := &commerceschema.SubscriptionLuckyReward{
		Id: 9962, DrawId: draw.Id, UserId: 9963, BlindBoxOpenRecordId: 9964,
		ParticipationType: "blind_box", LuckyNumber: "1702", MatchedDigits: 2,
		FinalRewardQuota: int64(platformruntime.QuotaPerUnit), CreditStatus: commerceschema.SubscriptionLuckyRewardCreditCredited,
	}
	require.NoError(t, db.Create(draw).Error)
	require.NoError(t, db.Create(reward).Error)
	require.NoError(t, db.Migrator().DropTable(&commerceschema.SubscriptionLuckyNumber{}))

	page, err := ListDailyLuckyNumberPublicWins(1, 20, "")
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Records, 1)
	require.Equal(t, "**02", page.Records[0].LuckySuffix)
}

func TestListDailyLuckyNumberPublicWinsFiltersByDrawDate(t *testing.T) {
	prepareDailyLuckyNumberTestDB(t)
	db := platformDBForDailyLuckyTest(t)
	draws := []commerceschema.SubscriptionLuckyDraw{
		{Id: 9971, DrawDate: "2099-01-05", WinningNumber: "2402", Status: commerceschema.SubscriptionLuckyDrawStatusCompleted},
		{Id: 9972, DrawDate: "2099-01-06", WinningNumber: "5723", Status: commerceschema.SubscriptionLuckyDrawStatusCompleted},
	}
	rewards := []commerceschema.SubscriptionLuckyReward{
		{Id: 9973, DrawId: 9971, UserSubscriptionId: 9974, LuckyNumber: "1002", MatchedDigits: 1, CreditStatus: commerceschema.SubscriptionLuckyRewardCreditCredited},
		{Id: 9975, DrawId: 9972, UserSubscriptionId: 9976, LuckyNumber: "5723", MatchedDigits: 4, CreditStatus: commerceschema.SubscriptionLuckyRewardCreditCredited},
	}
	require.NoError(t, db.Create(&draws).Error)
	require.NoError(t, db.Create(&rewards).Error)

	page, err := ListDailyLuckyNumberPublicWins(1, 100, "2099-01-05")
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.Records, 1)
	require.Equal(t, 1, page.Records[0].MatchedDigits)
}
