package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupStatusLogIndexStatementUsesActualDatabaseDialect(t *testing.T) {
	postgres := groupStatusLogIndexStatement("postgres")
	require.Contains(t, postgres, "CREATE INDEX CONCURRENTLY IF NOT EXISTS")
	require.Contains(t, postgres, `"group"`)
	require.NotContains(t, postgres, "`group`")

	mysql := groupStatusLogIndexStatement("mysql")
	require.Contains(t, mysql, "`group`")

	sqlite := groupStatusLogIndexStatement("sqlite")
	require.Contains(t, sqlite, "CREATE INDEX IF NOT EXISTS")
	require.Contains(t, sqlite, "`group`")
}

func TestBillingRequestUsageIndexStatementUsesDialect(t *testing.T) {
	postgresStatements := billingRequestUsageIndexStatements("postgres")
	require.Len(t, postgresStatements, 2)
	postgres := strings.Join(postgresStatements, "\n")
	if !strings.Contains(postgres, "CREATE INDEX CONCURRENTLY") || !strings.Contains(postgres, "INCLUDE (actual_amount)") {
		t.Fatalf("unexpected postgres statement: %s", postgres)
	}
	require.Contains(t, postgres, "WHERE status = 'settled'")
	mysql := strings.Join(billingRequestUsageIndexStatements("mysql"), "\n")
	if strings.Contains(mysql, "CONCURRENTLY") || !strings.Contains(mysql, "billing_settlements") {
		t.Fatalf("unexpected mysql statement: %s", mysql)
	}
	sqlite := strings.Join(billingRequestUsageIndexStatements("sqlite"), "\n")
	if !strings.Contains(sqlite, "IF NOT EXISTS") || !strings.Contains(sqlite, "WHERE status") {
		t.Fatalf("unexpected sqlite statement: %s", sqlite)
	}
}
