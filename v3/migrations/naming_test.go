package migrations

import (
	"regexp"
	"strings"
	"testing"
)

// Acceptance #10: the v3 schema carries none of v2's legacy balance naming.
var forbidden = regexp.MustCompile(`(?i)\b\w*(quota|claude|unified)\w*\b`)

func TestSchemaHasNoLegacyBalanceNaming(t *testing.T) {
	names, err := Files()
	if err != nil || len(names) == 0 {
		t.Fatalf("no migrations: %v", err)
	}
	for _, name := range names {
		sql, err := Read(name)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(sql, "\n") {
			code, _, _ := strings.Cut(line, "--") // comments may mention v2 history
			if m := forbidden.FindString(code); m != "" {
				t.Errorf("%s:%d uses legacy name %q", name, i+1, m)
			}
		}
	}
}

func TestMigrationFilesAreOrderedAndTimestamped(t *testing.T) {
	names, _ := Files()
	pattern := regexp.MustCompile(`^\d{14}_[a-z0-9_]+\.sql$`)
	for _, n := range names {
		if !pattern.MatchString(n) {
			t.Errorf("migration %q must be named YYYYMMDDHHMMSS_name.sql", n)
		}
	}
}
