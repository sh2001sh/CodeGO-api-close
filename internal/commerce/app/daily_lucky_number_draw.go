package app

import (
	"errors"
	"math"
	"time"

	commercedomain "github.com/sh2001sh/new-api/internal/commerce/domain"
	luckysettings "github.com/sh2001sh/new-api/internal/commerce/luckysettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type luckyDrawParticipant struct {
	Subscription   commerceschema.UserSubscription
	Plan           commerceschema.SubscriptionPlan
	Number         commerceschema.SubscriptionLuckyNumber
	BlindBoxNumber *commerceschema.BlindBoxDailyLuckyNumber
}

func createDailyLuckyDraw(drawDate string, drawAt time.Time, setting luckysettings.Setting) (*commerceschema.SubscriptionLuckyDraw, error) {
	if !subscriptionLuckyNumberTableReady() {
		return nil, errors.New("daily lucky number migration is not applied")
	}
	var draw commerceschema.SubscriptionLuckyDraw
	err := platformdb.DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("draw_date = ?", drawDate).First(&draw)
		if query.Error == nil {
			return nil
		}
		if !errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return query.Error
		}

		jackpotBefore, err := latestJackpotAfterTx(tx, drawDate, setting.JackpotInitialUSD)
		if err != nil {
			return err
		}
		winningNumber, err := generateLuckyNumber()
		if err != nil {
			return err
		}
		draw = commerceschema.SubscriptionLuckyDraw{
			DrawDate:            drawDate,
			WinningNumber:       winningNumber,
			JackpotBefore:       jackpotBefore,
			JackpotAfter:        jackpotBefore,
			Status:              commerceschema.SubscriptionLuckyDrawStatusSettling,
			Timezone:            setting.Timezone,
			DrawHour:            setting.DrawHour,
			DrawMinute:          setting.DrawMinute,
			BaseReward1USD:      setting.BaseReward1USD,
			BaseReward2USD:      setting.BaseReward2USD,
			BaseReward3USD:      setting.BaseReward3USD,
			BaseReward4USD:      setting.BaseReward4USD,
			MultiplierLite:      setting.MultiplierLite,
			MultiplierStandard:  setting.MultiplierStandard,
			MultiplierPro:       setting.MultiplierPro,
			MultiplierUltra:     setting.MultiplierUltra,
			JackpotInitialUSD:   setting.JackpotInitialUSD,
			JackpotIncrementUSD: setting.JackpotIncrementUSD,
			JackpotCapUSD:       setting.JackpotCapUSD,
			CostPerUSD:          setting.CostPerUSD,
			MonthlyBudgetUSD:    setting.MonthlyBudgetUSD,
			DrawnAt:             platformruntime.GetTimestamp(),
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&draw).Error; err != nil {
			return err
		}
		if draw.Id == 0 {
			if err := tx.Where("draw_date = ?", drawDate).First(&draw).Error; err != nil {
				return err
			}
			return nil
		}

		participants, err := listLuckyDrawParticipantsTx(tx, drawAt.Unix())
		if err != nil {
			return err
		}
		fullMatches := 0
		for index := range participants {
			if luckyMatchDigits(participants[index].Number.LuckySuffix, winningNumber) == 4 {
				fullMatches++
			}
		}
		jackpotShare := float64(0)
		if fullMatches > 0 {
			jackpotShare = jackpotBefore / float64(fullMatches)
		}
		for index := range participants {
			participant := participants[index]
			matched := luckyMatchDigits(participant.Number.LuckySuffix, winningNumber)
			baseReward := luckyBaseRewardUSD(matched, settingFromDraw(&draw))
			multiplier := luckyTierMultiplier(luckyMembershipTier(&participant.Plan), settingFromDraw(&draw))
			jackpotReward := float64(0)
			if matched == 4 {
				jackpotReward = jackpotShare
			}
			finalUSD := roundLuckyUSD(baseReward*multiplier + jackpotReward)
			status := commerceschema.SubscriptionLuckyRewardCreditCredited
			if finalUSD > 0 {
				status = commerceschema.SubscriptionLuckyRewardCreditPending
			}
			participationType := "subscription"
			blindBoxOpenRecordID := 0
			userSubscriptionID := participant.Subscription.Id
			userID := participant.Subscription.UserId
			if participant.BlindBoxNumber != nil {
				participationType = "blind_box"
				blindBoxOpenRecordID = participant.BlindBoxNumber.BlindBoxOpenRecordId
				userSubscriptionID = -blindBoxOpenRecordID
				userID = participant.BlindBoxNumber.UserId
			}
			reward := &commerceschema.SubscriptionLuckyReward{
				DrawId:               draw.Id,
				UserSubscriptionId:   userSubscriptionID,
				BlindBoxOpenRecordId: blindBoxOpenRecordID,
				ParticipationType:    participationType,
				UserId:               userID,
				LuckyNumber:          participant.Number.LuckySuffix,
				MembershipTier:       luckyMembershipTier(&participant.Plan),
				MatchedDigits:        matched,
				BaseRewardUSD:        roundLuckyUSD(baseReward),
				TierMultiplier:       multiplier,
				JackpotRewardUSD:     roundLuckyUSD(jackpotReward),
				FinalRewardQuota:     luckyRewardQuota(finalUSD),
				CreditStatus:         status,
				CreditedAt:           platformruntime.GetTimestamp(),
			}
			if reward.FinalRewardQuota > 0 {
				reward.CreditedAt = 0
			}
			if err := tx.Create(reward).Error; err != nil {
				return err
			}
		}
		draw.FullMatchCount = fullMatches
		draw.JackpotAfter = luckyJackpotAfter(jackpotBefore, fullMatches, settingFromDraw(&draw))
		return tx.Save(&draw).Error
	})
	if err != nil {
		return nil, err
	}
	return &draw, nil
}

func settingFromDraw(draw *commerceschema.SubscriptionLuckyDraw) luckysettings.Setting {
	if draw == nil {
		return luckysettings.Get()
	}
	return luckysettings.Setting{
		Timezone:            draw.Timezone,
		DrawHour:            draw.DrawHour,
		DrawMinute:          draw.DrawMinute,
		BaseReward1USD:      draw.BaseReward1USD,
		BaseReward2USD:      draw.BaseReward2USD,
		BaseReward3USD:      draw.BaseReward3USD,
		BaseReward4USD:      draw.BaseReward4USD,
		MultiplierLite:      draw.MultiplierLite,
		MultiplierStandard:  draw.MultiplierStandard,
		MultiplierPro:       draw.MultiplierPro,
		MultiplierUltra:     draw.MultiplierUltra,
		JackpotInitialUSD:   draw.JackpotInitialUSD,
		JackpotIncrementUSD: draw.JackpotIncrementUSD,
		JackpotCapUSD:       draw.JackpotCapUSD,
		CostPerUSD:          draw.CostPerUSD,
		MonthlyBudgetUSD:    draw.MonthlyBudgetUSD,
	}
}

func latestJackpotAfterTx(tx *gorm.DB, beforeDate string, initial float64) (float64, error) {
	var previous commerceschema.SubscriptionLuckyDraw
	err := tx.Where("draw_date < ?", beforeDate).Order("draw_date desc, id desc").First(&previous).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return initial, nil
	}
	if err != nil {
		return 0, err
	}
	return previous.JackpotAfter, nil
}

func listLuckyDrawParticipantsTx(tx *gorm.DB, drawUnix int64) ([]luckyDrawParticipant, error) {
	var subscriptions []commerceschema.UserSubscription
	if err := tx.Where("status = ? AND start_time <= ? AND end_time > ?", "active", drawUnix, drawUnix).
		Order("id asc").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	if len(subscriptions) == 0 {
		return appendBlindBoxLuckyParticipantsTx(tx, nil, drawUnix)
	}
	planIDs := make([]int, 0, len(subscriptions))
	for _, sub := range subscriptions {
		planIDs = append(planIDs, sub.PlanId)
	}
	var plans []commerceschema.SubscriptionPlan
	if err := tx.Where("id IN ?", planIDs).Find(&plans).Error; err != nil {
		return nil, err
	}
	planMap := make(map[int]commerceschema.SubscriptionPlan, len(plans))
	for _, plan := range plans {
		planMap[plan.Id] = plan
	}
	subscriptionIDs := make([]int, 0, len(subscriptions))
	for index := range subscriptions {
		sub := &subscriptions[index]
		plan, ok := planMap[sub.PlanId]
		if !ok || commercedomain.NormalizeSubscriptionPlanType(plan.PlanType) != commerceschema.SubscriptionPlanTypeMonthly || !plan.LuckyDrawEnabled {
			continue
		}
		// Backfilled/admin-created subscriptions may not have a number yet.
		// Allocate it lazily before building the participant set so they can
		// enter the very draw being settled.
		if _, err := ensureSubscriptionLuckyNumberTx(tx, sub, &plan); err != nil {
			return nil, err
		}
		subscriptionIDs = append(subscriptionIDs, sub.Id)
	}
	if len(subscriptionIDs) == 0 {
		return appendBlindBoxLuckyParticipantsTx(tx, nil, drawUnix)
	}
	var numbers []commerceschema.SubscriptionLuckyNumber
	if err := tx.Where("user_subscription_id IN ?", subscriptionIDs).Find(&numbers).Error; err != nil {
		return nil, err
	}
	numberMap := make(map[int]commerceschema.SubscriptionLuckyNumber, len(numbers))
	for _, number := range numbers {
		numberMap[number.UserSubscriptionId] = number
	}
	participants := make([]luckyDrawParticipant, 0, len(subscriptionIDs))
	for _, sub := range subscriptions {
		plan, ok := planMap[sub.PlanId]
		if !ok || commercedomain.NormalizeSubscriptionPlanType(plan.PlanType) != commerceschema.SubscriptionPlanTypeMonthly || !plan.LuckyDrawEnabled {
			continue
		}
		number, ok := numberMap[sub.Id]
		if !ok || len(number.LuckySuffix) != 4 {
			continue
		}
		participants = append(participants, luckyDrawParticipant{Subscription: sub, Plan: plan, Number: number})
	}
	return appendBlindBoxLuckyParticipantsTx(tx, participants, drawUnix)
}

func appendBlindBoxLuckyParticipantsTx(tx *gorm.DB, participants []luckyDrawParticipant, drawUnix int64) ([]luckyDrawParticipant, error) {
	setting := luckysettings.Get()
	location, err := setting.Location()
	if err != nil {
		return nil, err
	}
	drawDate := time.Unix(drawUnix, 0).In(location).Format(luckyDrawDateLayout)
	var numbers []commerceschema.BlindBoxDailyLuckyNumber
	if err := tx.Where("draw_date = ? AND expires_at > ?", drawDate, drawUnix).Order("id asc").Find(&numbers).Error; err != nil {
		return nil, err
	}
	for index := range numbers {
		number := numbers[index]
		participants = append(participants, luckyDrawParticipant{
			Plan:           commerceschema.SubscriptionPlan{MembershipTier: commerceschema.SubscriptionMembershipTierNone},
			Number:         commerceschema.SubscriptionLuckyNumber{LuckySuffix: number.LuckySuffix},
			BlindBoxNumber: &number,
		})
	}
	return participants, nil
}

func roundLuckyUSD(value float64) float64 {
	return math.Round(value*100) / 100
}
