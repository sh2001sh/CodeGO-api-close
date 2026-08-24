package store

import (
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
