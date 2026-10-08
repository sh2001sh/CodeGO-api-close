//go:build pgintegration

package desktop

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestDeviceApprovalIsSingleWinnerScopedAndRevocable(t *testing.T) {
	s, a, b := desktopFixture(t)
	ctx := context.Background()
	start, err := s.Start(ctx, StartInput{DeviceName: "Laptop", Platform: "windows", AppVersion: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if start.ExpiresIn != 600 || start.Interval != 5 {
		t.Fatal("invalid auth contract")
	}
	if _, err := s.View(ctx, start.SessionID, "WRONG"); !errors.Is(err, ErrMissing) {
		t.Fatal("wrong code accepted", err)
	}
	view, err := s.View(ctx, start.SessionID, start.UserCode)
	if err != nil || view.Status != "pending" {
		t.Fatal("view", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() { _, err := s.Decide(ctx, a.ID, start.SessionID, true); results <- err })
	}
	wg.Wait()
	close(results)
	wins, denials := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrDenied) {
			denials++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || denials != 1 {
		t.Fatalf("wins %d denials %d", wins, denials)
	}
	poll, err := s.Poll(ctx, start.SessionID)
	if err != nil || !poll.Authenticated || poll.UserID != a.ID {
		t.Fatal("poll", err)
	}
	r := httptest.NewRequest("GET", "/api/desktop/account/summary", nil)
	r.Header.Set("Authorization", "Bearer "+poll.AccessToken)
	d, err := s.Authenticate(r, "account:read")
	if err != nil || d.UserID != a.ID {
		t.Fatal("device auth", err)
	}
	if _, err := s.Authenticate(r, "admin:write"); !errors.Is(err, ErrDenied) {
		t.Fatal("unknown scope accepted", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE v3_identity.desktop_devices SET scopes=ARRAY['account:read'] WHERE id=$1`, d.ID); err != nil {
		t.Fatal(err)
	}
	deniedRequest := httptest.NewRequest("GET", "/api/desktop/tokens", nil)
	deniedRequest.Header = r.Header.Clone()
	deniedResponse := httptest.NewRecorder()
	s.Handler().ServeHTTP(deniedResponse, deniedRequest)
	if deniedResponse.Code != http.StatusForbidden {
		t.Fatalf("valid device lacking scope: %d %s", deniedResponse.Code, deniedResponse.Body.String())
	}
	if _, err := s.id.AuthenticateRequest(r); err == nil {
		t.Fatal("desktop token became browser/admin session")
	}
	if err := s.Revoke(ctx, b.ID, d.ID); !errors.Is(err, ErrMissing) {
		t.Fatal("other user revoked device", err)
	}
	if err := s.Revoke(ctx, a.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(r, "account:read"); !errors.Is(err, ErrDenied) {
		t.Fatal("revoked device accepted", err)
	}
	deniedResponse = httptest.NewRecorder()
	s.Handler().ServeHTTP(deniedResponse, r)
	if deniedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("revoked device: %d %s", deniedResponse.Code, deniedResponse.Body.String())
	}
	poll, err = s.Poll(ctx, start.SessionID)
	if err != nil || poll.Authenticated || poll.AccessToken != "" {
		t.Fatal("poll leaked revoked token", err)
	}
}
func TestExpiredAndRejectedDesktopSessionsNeverIssueToken(t *testing.T) {
	s, a, _ := desktopFixture(t)
	ctx := context.Background()
	now := time.Now()
	s.cfg.Now = func() time.Time { return now }
	start, err := s.Start(ctx, StartInput{})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(11 * time.Minute)
	if _, err := s.Decide(ctx, a.ID, start.SessionID, true); !errors.Is(err, ErrDenied) {
		t.Fatal("expired accepted", err)
	}
	poll, err := s.Poll(ctx, start.SessionID)
	if err != nil || poll.Status != "expired" || poll.AccessToken != "" {
		t.Fatal("expired token", err)
	}
	start, err = s.Start(ctx, StartInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(ctx, a.ID, start.SessionID, false); err != nil {
		t.Fatal(err)
	}
	poll, err = s.Poll(ctx, start.SessionID)
	if err != nil || poll.Status != "rejected" || poll.Authenticated {
		t.Fatal("rejected token", err)
	}
}
