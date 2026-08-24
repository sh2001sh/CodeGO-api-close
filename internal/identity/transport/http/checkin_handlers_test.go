package http

import (
	"github.com/sh2001sh/new-api/constant"
	identityapp "github.com/sh2001sh/new-api/internal/identity/app"
	identitydomain "github.com/sh2001sh/new-api/internal/identity/domain"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	identitystore "github.com/sh2001sh/new-api/internal/identity/store"
	platformencoding "github.com/sh2001sh/new-api/internal/platform/encodingx"
	"net/http"
	"testing"
	"time"
)

func TestGetUserCheckinStatusReturnsMonthlyStats(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	restoreCheckinSetting := snapshotCheckinSetting()
	t.Cleanup(restoreCheckinSetting)

	setting := identitystore.GetCheckinSetting()
	setting.Enabled = true
	setting.MinQuota = 100
	setting.MaxQuota = 200

	user := &identityschema.User{Id: 1, Username: "checkin-user", Password: "password123", DisplayName: "Checkin User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default"}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	record := &identitydomain.Checkin{
		UserId:       user.Id,
		CheckinDate:  time.Now().Format("2006-01-02"),
		QuotaAwarded: 150,
		CreatedAt:    time.Now().Unix(),
	}
	if err := db.Create(record).Error; err != nil {
		t.Fatalf("failed to seed checkin record: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/user/checkin", nil, user.Id)
	GetUserCheckinStatus(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success {
		t.Fatalf("expected success response, got message: %s", response.Message)
	}

	var payload struct {
		Enabled  bool           `json:"enabled"`
		MinQuota int            `json:"min_quota"`
		MaxQuota int            `json:"max_quota"`
		Stats    map[string]any `json:"stats"`
	}
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode checkin status: %v", err)
	}
	if !payload.Enabled || payload.MinQuota != 100 || payload.MaxQuota != 200 {
		t.Fatalf("unexpected checkin status payload: %#v", payload)
	}
	if checkedInToday, ok := payload.Stats["checked_in_today"].(bool); !ok || !checkedInToday {
		t.Fatalf("expected checked_in_today=true, got %#v", payload.Stats["checked_in_today"])
	}
}

func TestDoUserCheckinAwardsQuotaAndCreatesRecord(t *testing.T) {
	db := setupDesktopHTTPTestDB(t)
	restoreCheckinSetting := snapshotCheckinSetting()
	t.Cleanup(restoreCheckinSetting)

	setting := identitystore.GetCheckinSetting()
	setting.Enabled = true
	setting.MinQuota = 120
	setting.MaxQuota = 120

	user := &identityschema.User{Id: 1, Username: "checkin-award-user", Password: "password123", DisplayName: "Checkin Award User", Role: constant.RoleCommonUser, Status: constant.UserStatusEnabled, Group: "default", ClaudeQuota: 500}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/user/checkin", nil, user.Id)
	DoUserCheckin(ctx)

	response := decodeAPIResponse(t, recorder)
	if !response.Success || response.Message != "签到成功" {
		t.Fatalf("expected successful checkin response, got %#v", response)
	}

	var payload struct {
		QuotaAwarded int    `json:"quota_awarded"`
		CheckinDate  string `json:"checkin_date"`
	}
	if err := platformencoding.Unmarshal(response.Data, &payload); err != nil {
		t.Fatalf("failed to decode checkin result: %v", err)
	}
	if payload.QuotaAwarded != 120 || payload.CheckinDate == "" {
		t.Fatalf("unexpected checkin result payload: %#v", payload)
	}

	reloaded, err := loadUserByIDForTest(user.Id, true)
	if err != nil {
		t.Fatalf("failed to reload user: %v", err)
	}
	if reloaded.ClaudeQuota != 620 {
		t.Fatalf("expected quota to increase to 620, got %d", reloaded.ClaudeQuota)
	}

	status, err := identityapp.LoadCheckinStatus(user.Id, time.Now().Format("2006-01"))
	if err != nil {
		t.Fatalf("failed to load checkin status: %v", err)
	}
	if totalCheckins, ok := status.Stats["total_checkins"].(int64); !ok || totalCheckins != 1 {
		t.Fatalf("expected one persisted checkin, got %#v", status.Stats["total_checkins"])
	}
}

func snapshotCheckinSetting() func() {
	current := *identitystore.GetCheckinSetting()
	return func() {
		*identitystore.GetCheckinSetting() = current
	}
}
