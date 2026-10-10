package store

import (
	"fmt"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	platformruntime "github.com/sh2001sh/new-api/internal/platform/runtime"

	"gorm.io/gorm"
)

func getDBTimestampFrom(db *gorm.DB, postgresQuery string) (int64, error) {
	if db == nil {
		return 0, fmt.Errorf("database time transaction is nil")
	}
	var ts int64
	var err error
	switch {
	case platformdb.UsingPostgreSQL:
		err = db.Raw(postgresQuery).Scan(&ts).Error
	case platformdb.UsingSQLite:
		err = db.Raw("SELECT strftime('%s','now')").Scan(&ts).Error
	default:
		err = db.Raw("SELECT UNIX_TIMESTAMP()").Scan(&ts).Error
	}
	if err != nil {
		return 0, err
	}
	if ts <= 0 {
		return 0, fmt.Errorf("invalid database timestamp: %d", ts)
	}
	return ts, nil
}

// GetDBTimestampTx reads current database time using the supplied transaction
// without borrowing another connection or falling back to application time.
// PostgreSQL clock_timestamp advances even when the transaction waited on a lock.
func GetDBTimestampTx(tx *gorm.DB) (int64, error) {
	return getDBTimestampFrom(tx, "SELECT EXTRACT(EPOCH FROM clock_timestamp())::bigint")
}

// GetDBTimestamp returns a UNIX timestamp from database time.
// Falls back to application time on error.
func GetDBTimestamp() int64 {
	ts, err := getDBTimestampFrom(platformdb.DB, "SELECT EXTRACT(EPOCH FROM NOW())::bigint")
	if err != nil {
		return platformruntime.GetTimestamp()
	}
	return ts
}
