package pg

import (
	"context"
	"strings"
	"testing"
)

// Connect must reject bad configuration before any network round trip, so a
// misconfigured service fails at start with a clear message.
func TestConnectRejectsBadConfigWithoutDialing(t *testing.T) {
	cases := []struct {
		name, dsn, want string
	}{
		{"empty", "", "empty DSN"},
		{"unparsable", "postgres://user:pa ss@[::1:5432/db", "parse dsn"},
	}
	for _, c := range cases {
		_, err := Connect(context.Background(), Config{DSN: c.dsn})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Connect = %v; want error containing %q", c.name, err, c.want)
		}
	}
}
