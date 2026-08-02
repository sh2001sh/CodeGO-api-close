package http

import (
	stdhttp "net/http"
	"testing"
	"time"

	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"
)

func TestGetBlindBoxHistoryReturnsThirtyDayPage(t *testing.T) {
	db := setupCommerceHTTPTestDB(t)
	if err := db.AutoMigrate(&commerceschema.BlindBoxOpenRecord{}); err != nil {
		t.Fatalf("failed to migrate blind box history tables: %v", err)
	}
	now := platformruntime.GetTimestamp()
	if err := db.Create(&commerceschema.BlindBoxOpenRecord{
		UserId:      701,
		RewardType:  commerceschema.BlindBoxRewardTypeQuota,
		RewardUSD:   8,
		RewardTitle: "8.00 美元奖励",
		CreateTime:  now,
	}).Error; err != nil {
		t.Fatalf("failed to seed blind box record: %v", err)
	}
	if err := db.Create(&commerceschema.BlindBoxOpenRecord{
		UserId:      701,
		RewardType:  commerceschema.BlindBoxRewardTypeQuota,
		RewardTitle: "expired history",
		CreateTime:  now - int64(31*24*time.Hour/time.Second),
	}).Error; err != nil {
		t.Fatalf("failed to seed expired blind box record: %v", err)
	}

	ctx, recorder := newCommerceContext(t, stdhttp.MethodGet, "/api/blind-box/history?p=1&page_size=20", nil, 701)
	getBlindBoxHistory(ctx)
	response := decodeCommerceResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected blind box history success, got %#v", response)
	}
	var payload struct {
		Total         int64                               `json:"total"`
		RetentionDays int                                 `json:"retention_days"`
		Records       []commerceschema.BlindBoxOpenRecord `json:"records"`
	}
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode blind box history: %v", err)
	}
	if payload.Total != 1 || payload.RetentionDays != 30 || len(payload.Records) != 1 {
		t.Fatalf("unexpected blind box history payload: %#v", payload)
	}
}
