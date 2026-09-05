package store

import (
	"strings"
	"testing"

	auditschema "github.com/sh2001sh/new-api/internal/audit/schema"
	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	marketplaceschema "github.com/sh2001sh/new-api/internal/marketplace/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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

func TestChannelWindowLogIndexStatementUsesDialect(t *testing.T) {
	postgres := channelWindowLogIndexStatement("postgres")
	require.Contains(t, postgres, "CREATE INDEX CONCURRENTLY IF NOT EXISTS")
	require.Contains(t, postgres, "(channel_id, created_at DESC, id DESC)")
	require.Contains(t, postgres, "WHERE type IN")

	mysql := channelWindowLogIndexStatement("mysql")
	require.Contains(t, mysql, "(channel_id, type, created_at, id)")
	require.NotContains(t, mysql, "CONCURRENTLY")

	sqlite := channelWindowLogIndexStatement("sqlite")
	require.Contains(t, sqlite, "CREATE INDEX IF NOT EXISTS")
}

func TestQueryPathIndexStatementsUseDialect(t *testing.T) {
	postgres := queryPathIndexStatements("postgres")
	require.Len(t, postgres, 3)
	require.Contains(t, postgres[0].SQL, "CREATE INDEX CONCURRENTLY IF NOT EXISTS")
	require.Contains(t, postgres[0].SQL, "WHERE type IN")
	require.Contains(t, postgres[1].SQL, "gateway.request_attempt_audits")
	require.Contains(t, postgres[2].SQL, "marketplace.settlements")

	mysql := queryPathIndexStatements("mysql")
	require.Len(t, mysql, 3)
	require.NotContains(t, strings.Join([]string{mysql[0].SQL, mysql[1].SQL, mysql[2].SQL}, "\n"), "CONCURRENTLY")

	sqliteStatements := queryPathIndexStatements("sqlite")
	require.Contains(t, sqliteStatements[0].SQL, "CREATE INDEX IF NOT EXISTS")
}

func TestMigrateQueryPathIndexesSQLiteIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)

	originalDB, originalLogDB, originalPostgreSQL := platformdb.DB, platformdb.LogDB, platformdb.UsingPostgreSQL
	t.Cleanup(func() {
		platformdb.DB = originalDB
		platformdb.LogDB = originalLogDB
		platformdb.UsingPostgreSQL = originalPostgreSQL
	})
	platformdb.DB = db
	platformdb.LogDB = db
	platformdb.UsingPostgreSQL = false

	require.NoError(t, db.AutoMigrate(&auditschema.Log{}, &gatewayschema.RequestAttemptAudit{}, &marketplaceschema.Settlement{}))
	require.NoError(t, migrateQueryPathIndexes(db))
	require.NoError(t, migrateQueryPathIndexes(db))
	require.True(t, db.Migrator().HasIndex("logs", "idx_logs_channel_type_created_id"))
	require.True(t, db.Migrator().HasIndex(&gatewayschema.RequestAttemptAudit{}, "idx_request_attempt_audit_channel_started"))
	require.True(t, db.Migrator().HasIndex(&marketplaceschema.Settlement{}, "idx_marketplace_settlements_owner_group_created"))
}
