//go:build pgintegration

package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/sh2001sh/new-api/v3/internal/desktop"
)

func TestRestoreComposedDesktopApprovalScopeAndRevocation(t *testing.T) {
	s := restoreStack(t, nil)
	alice, bob := s.register(t, "restored_desktop_alice"), s.register(t, "restored_desktop_bob")
	started := restoreData[desktop.StartResult](t, s.call(t, "POST", "/api/desktop/auth/session", "", `{"device_name":"Restored Desktop","platform":"windows"}`, 200))
	body := fmt.Sprintf(`{"session_id":%q}`, started.SessionID)
	view := "/api/desktop/auth/session?session_id=" + started.SessionID + "&code=" + started.UserCode
	s.call(t, "GET", view, "", "", 401)
	s.call(t, "GET", view, alice.AccessToken, "", 200)
	s.call(t, "POST", "/api/desktop/auth/approve", "", body, 401)
	s.call(t, "POST", "/api/desktop/auth/approve", alice.AccessToken, body, 200)
	s.call(t, "POST", "/api/desktop/auth/approve", bob.AccessToken, body, 403)
	poll := restoreData[desktop.PollResult](t, s.call(t, "POST", "/api/desktop/auth/poll", "", body, 200))
	if !poll.Authenticated || poll.UserID != alice.User.ID || poll.DeviceID <= 0 || poll.AccessToken == "" {
		t.Fatalf("approved device not issued: %+v", poll)
	}
	device := poll.AccessToken
	s.call(t, "GET", "/api/desktop/account/summary", device, "", 200)
	s.call(t, "GET", "/api/desktop/account/summary", alice.AccessToken, "", 401)
	s.call(t, "GET", "/api/user/self", device, "", 401)
	s.call(t, "GET", "/api/catalog/channels", device, "", 401)
	verifyRestoreDesktopImport(t, s, alice.AccessToken, bob.AccessToken, device)
	if _, err := s.pool.Exec(context.Background(), `UPDATE v3_identity.desktop_devices SET scopes=ARRAY['account:read']::text[] WHERE id=$1`, poll.DeviceID); err != nil {
		t.Fatal(err)
	}
	s.call(t, "GET", "/api/desktop/account/summary", device, "", 200)
	s.call(t, "GET", "/api/desktop/tokens", device, "", 403)
	s.call(t, "GET", "/api/desktop/usage/logs", device, "", 403)
	path := fmt.Sprintf("/api/desktop/devices/%d", poll.DeviceID)
	s.call(t, "DELETE", path, bob.AccessToken, "", 404)
	s.call(t, "DELETE", path, alice.AccessToken, "", 200)
	s.call(t, "GET", "/api/desktop/account/summary", device, "", 401)
	poll = restoreData[desktop.PollResult](t, s.call(t, "POST", "/api/desktop/auth/poll", "", body, 200))
	if poll.Authenticated || poll.AccessToken != "" || poll.Status != "rejected" {
		t.Fatalf("revoked device still pollable: %+v", poll)
	}
}

func verifyRestoreDesktopImport(t *testing.T, s *restoredStack, alice, bob, device string) {
	t.Helper()
	type ensured struct {
		Token struct {
			ID int64 `json:"id"`
		} `json:"token"`
		Created bool   `json:"created"`
		Key     string `json:"full_key"`
	}
	first := restoreData[ensured](t, s.call(t, "POST", "/api/desktop/tokens/ensure", device, `{"device_name":"Restore","group":"default"}`, 200))
	second := restoreData[ensured](t, s.call(t, "POST", "/api/desktop/tokens/ensure", device, `{"device_name":"Restore","group":"default"}`, 200))
	if !first.Created || second.Created || first.Token.ID <= 0 || second.Token.ID != first.Token.ID || first.Key == "" || second.Key != first.Key {
		t.Fatal("desktop ensure did not preserve one actual key")
	}
	body := fmt.Sprintf(`{"token_id":%d,"tool":"codex","target":"codego"}`, first.Token.ID)
	s.call(t, "POST", "/api/desktop/import/deeplink", bob, body, 404)
	created := restoreData[struct {
		Code string `json:"code"`
	}](t, s.call(t, "POST", "/api/desktop/import/deeplink", alice, body, 200))
	if created.Code == "" {
		t.Fatal("desktop import missing one-time code")
	}
	path := "/api/desktop/import/config?code=" + created.Code
	config := restoreData[desktop.ImportPayload](t, s.call(t, "GET", path, "", "", 200))
	if config.APIKey != first.Key || config.Tool != "codex" || config.Endpoint != "http://control.test/v1" {
		t.Fatal("desktop import changed actual key or provider configuration")
	}
	s.call(t, "GET", path, "", "", 404)
	for _, path := range []string{"/api/desktop/config/template?tool=codex", "/api/desktop/pricing", "/api/desktop/groups", "/api/desktop/group-status", "/api/desktop/usage/logs?p=2", "/api/desktop/usage/trends?days=30", "/api/desktop/service/status"} {
		s.call(t, "GET", path, device, "", 200)
	}
	s.call(t, "GET", "/api/desktop/usage/trends?days=31", device, "", 400)
}
