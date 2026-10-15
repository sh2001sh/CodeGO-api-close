package http

import (
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	"github.com/gin-gonic/gin"
	"github.com/sh2001sh/new-api/constant"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
	stdhttp "net/http"
	"testing"
)

func TestListGroupBuysReturnsActiveRooms(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)

	user := &identityschema.User{
		Id:          1,
		Username:    "group-buy-list",
		Password:    "password123",
		DisplayName: "Group Buy List",
		Role:        constant.RoleCommonUser,
		Status:      constant.UserStatusEnabled,
		Group:       "default",
		AffCode:     "GBL1",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	plan := &commerceschema.SubscriptionPlan{
		Id:              1,
		Title:           "Lite月卡",
		Enabled:         true,
		GroupBuyEnabled: true,
		PriceAmount:     29,
		Currency:        "USD",
		DurationUnit:    commerceschema.SubscriptionDurationMonth,
		DurationValue:   1,
		TotalAmount:     int64(platformruntime.QuotaPerUnit * 50),
		GroupBuyBonus2:  10,
		GroupBuyBonus3:  20,
		GroupBuyBonus5:  30,
	}
	if err := db.Create(plan).Error; err != nil {
		t.Fatalf("failed to seed plan: %v", err)
	}
	order := &commerceschema.GroupBuyOrder{
		Id:           1,
		InitiatorId:  user.Id,
		PlanId:       plan.Id,
		Status:       commerceschema.GroupBuyStatusPending,
		TargetCount:  5,
		CurrentCount: 1,
		ExpiresAt:    platformruntime.GetTimestamp() + 3600,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("failed to seed group buy order: %v", err)
	}
	if err := db.Create(&commerceschema.GroupBuyMember{
		Id:         1,
		GroupBuyId: order.Id,
		UserId:     user.Id,
		OrderId:    1,
	}).Error; err != nil {
		t.Fatalf("failed to seed group buy member: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodGet, "/api/group-buy/list", nil, user.Id)
	listGroupBuys(ctx)

	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected group buy list success, got %#v", response)
	}
}

func TestGetGroupBuyReturnsDetail(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)

	plan := &commerceschema.SubscriptionPlan{
		Id:              10,
		Title:           "Pro月卡",
		Enabled:         true,
		GroupBuyEnabled: true,
		PriceAmount:     99,
		Currency:        "USD",
		DurationUnit:    commerceschema.SubscriptionDurationMonth,
		DurationValue:   1,
		TotalAmount:     int64(platformruntime.QuotaPerUnit * 100),
		GroupBuyBonus2:  20,
		GroupBuyBonus3:  35,
		GroupBuyBonus5:  50,
	}
	if err := db.Create(plan).Error; err != nil {
		t.Fatalf("failed to seed plan: %v", err)
	}
	order := &commerceschema.GroupBuyOrder{
		Id:           11,
		InitiatorId:  7,
		PlanId:       plan.Id,
		Status:       commerceschema.GroupBuyStatusPending,
		TargetCount:  5,
		CurrentCount: 1,
		ExpiresAt:    platformruntime.GetTimestamp() + 3600,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("failed to seed order: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodGet, "/api/group-buy/11", nil, 0)
	ctx.Params = gin.Params{{Key: "id", Value: "11"}}
	getGroupBuy(ctx)

	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected group buy detail success, got %#v", response)
	}
}

func TestJoinGroupBuyCreatesMember(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)

	user := &identityschema.User{
		Id:          21,
		Username:    "group-buy-joiner",
		Password:    "password123",
		DisplayName: "Group Buy Joiner",
		Role:        constant.RoleCommonUser,
		Status:      constant.UserStatusEnabled,
		Group:       "default",
		AffCode:     "GBJ1",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	plan := &commerceschema.SubscriptionPlan{
		Id:              22,
		Title:           "Standard月卡",
		Enabled:         true,
		GroupBuyEnabled: true,
		PriceAmount:     59,
		Currency:        "USD",
		DurationUnit:    commerceschema.SubscriptionDurationMonth,
		DurationValue:   1,
		TotalAmount:     int64(platformruntime.QuotaPerUnit * 80),
		GroupBuyBonus2:  20,
	}
	if err := db.Create(plan).Error; err != nil {
		t.Fatalf("failed to seed plan: %v", err)
	}
	order := &commerceschema.GroupBuyOrder{
		Id:           23,
		InitiatorId:  99,
		PlanId:       plan.Id,
		Status:       commerceschema.GroupBuyStatusPending,
		TargetCount:  5,
		CurrentCount: 1,
		ExpiresAt:    platformruntime.GetTimestamp() + 3600,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatalf("failed to seed order: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodPost, "/api/group-buy/join", map[string]any{
		"group_buy_id": order.Id,
		"order_id":     88,
	}, user.Id)
	joinGroupBuy(ctx)

	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected join group buy success, got %#v", response)
	}

	var memberCount int64
	if err := db.Model(&commerceschema.GroupBuyMember{}).
		Where("group_buy_id = ? AND user_id = ?", order.Id, user.Id).
		Count(&memberCount).Error; err != nil {
		t.Fatalf("failed to count group buy members: %v", err)
	}
	if memberCount != 1 {
		t.Fatalf("expected created group buy member, got %d", memberCount)
	}
}
