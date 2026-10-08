package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type unavailableAccountSender struct{ err error }

func (s unavailableAccountSender) Available(context.Context) (bool, error) { return false, s.err }
func (unavailableAccountSender) SendAccountEmail(context.Context, string, string, string) error {
	panic("unavailable email sender must not deliver")
}

func TestStoredEmailUnavailablePrecedesAccountLookup(t *testing.T) {
	for _, path := range []string{"/api/verification?email=unknown@example.test", "/api/reset_password?email=unknown@example.test"} {
		t.Run(path, func(t *testing.T) {
			c, _ := testControl(t) // No database: unavailable mail must fail before account lookup.
			c.cfg.EmailSender = unavailableAccountSender{}
			w := httptest.NewRecorder()
			c.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("unconfigured mail status=%d", w.Code)
			}
		})
	}
	c, _ := testControl(t)
	want := errors.New("stored mail configuration cannot be read")
	c.cfg.EmailSender = unavailableAccountSender{err: want}
	if err := c.SendPasswordReset(context.Background(), "unknown@example.test"); !errors.Is(err, want) {
		t.Fatalf("configuration failure was hidden: %v", err)
	}
}
